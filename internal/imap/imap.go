// Package imap provides IMAP client functionality for fetching and processing emails.
package imap

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	"github.com/rs/zerolog/log"
)

const (
	// maxChannelBuffer defines the buffer size for IMAP message channels
	maxChannelBuffer = 50

	// maxRegexGroups expected from ticket ID regex matching
	maxRegexGroups = 3

	// maxHeaderLines to log during email parsing debug
	maxHeaderLines = 10

	// attachmentDisposition constant values
	attachmentDisposition = "attachment"
	inlineDisposition     = "inline"

	// base64Encoding constant value
	base64Encoding = "base64"

	// Connection retry constants
	maxRetryAttempts = 3
	retryDelay       = 5 * time.Second

	// Byte boundaries for charset decoding
	asciiUpperBound     = 0x80 // Bytes below this are ASCII
	latin1ExtendedStart = 0xA0 // Start of Latin-1 extended characters
)

// Config holds IMAP client configuration parameters.
type Config struct {
	Host, Username, Password, Folder, SearchTo, ProcessedKeyword string
	Port                                                         int
}

// Client represents an IMAP email client connection.
type Client struct {
	cfg     Config
	c       *client.Client
	mailbox string
}

// New creates a new IMAP client with the given configuration.
func New(cfg Config) (*Client, error) {
	cl := &Client{cfg: cfg}
	err := cl.connect()
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// connect establishes a connection to the IMAP server
func (cl *Client) connect() error {
	addr := net.JoinHostPort(cl.cfg.Host, itoa(cl.cfg.Port))
	tlsConf := &tls.Config{
		ServerName: cl.cfg.Host,
		MinVersion: tls.VersionTLS12,
	}
	c, err := client.DialTLS(addr, tlsConf)
	if err != nil {
		return err
	}
	if err := c.Login(cl.cfg.Username, cl.cfg.Password); err != nil {
		_ = c.Logout()
		return err
	}
	cl.c = c
	if err := cl.selectFolder(cl.cfg.Folder); err != nil {
		_ = c.Logout()
		return err
	}
	log.Debug().Str("host", cl.cfg.Host).Str("folder", cl.cfg.Folder).Msg("IMAP connection established")
	return nil
}

// Close closes the IMAP connection and logs out from the server.
func (cl *Client) Close() error {
	if cl.c != nil {
		_ = cl.c.Logout()
	}
	return nil
}

// isConnectionError checks if the error is related to connection issues
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	connectionErrors := []string{
		"connection closed",
		"broken pipe",
		"connection reset",
		"no route to host",
		"network is unreachable",
		"connection timed out",
		"connection refused",
		"eof",
	}

	for _, connErr := range connectionErrors {
		if strings.Contains(errStr, connErr) {
			return true
		}
	}
	return false
}

// reconnect attempts to reconnect to the IMAP server with retry logic
func (cl *Client) reconnect() error {
	log.Warn().Msg("attempting to reconnect to IMAP server")

	// Close existing connection
	if cl.c != nil {
		_ = cl.c.Logout()
		cl.c = nil
	}

	// Retry connection with backoff
	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		log.Debug().Int("attempt", attempt).Int("max_attempts", maxRetryAttempts).Msg("reconnection attempt")

		err := cl.connect()
		if err == nil {
			log.Info().Int("attempt", attempt).Msg("IMAP reconnection successful")
			return nil
		}

		log.Warn().Err(err).Int("attempt", attempt).Msg("reconnection attempt failed")

		if attempt < maxRetryAttempts {
			log.Debug().Dur("delay", retryDelay).Msg("waiting before next reconnection attempt")
			time.Sleep(retryDelay)
		}
	}

	return fmt.Errorf("failed to reconnect after %d attempts", maxRetryAttempts)
}

func (cl *Client) selectFolder(name string) error {
	_, err := cl.c.Select(name, false)
	cl.mailbox = name
	return err
}

// Attachment represents an email attachment with its metadata and content.
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	Data        []byte
}

// Email represents a parsed email message with attachments.
type Email struct {
	ID          string // composed "mbox-uid"
	UID         uint32
	Subject     string
	FromName    string
	FromEmail   string
	Body        string // prefer text/plain; fallback to text/html stripped
	Attachments []Attachment
}

// FetchUnseen retrieves all unseen emails from the configured IMAP folder.
func (cl *Client) FetchUnseen(ctx context.Context) ([]Email, error) {
	return cl.fetchUnseenWithRetry(ctx, 0)
}

// fetchUnseenWithRetry implements the FetchUnseen logic with automatic reconnection
func (cl *Client) fetchUnseenWithRetry(ctx context.Context, retryCount int) ([]Email, error) {
	log.Debug().Str("folder", cl.cfg.Folder).Str("search_to", cl.cfg.SearchTo).Msg("searching for unseen emails")

	// First, get mailbox status
	status, err := cl.c.Status(cl.cfg.Folder, []imap.StatusItem{imap.StatusMessages, imap.StatusUnseen})
	if err != nil && isConnectionError(err) {
		return cl.handleFetchError(ctx, err, retryCount)
	}
	if err == nil {
		log.Debug().Uint32("total_messages", status.Messages).Uint32("unseen_count", status.Unseen).Msg("mailbox status")
	}

	crit := imap.NewSearchCriteria()
	crit.WithoutFlags = []string{imap.SeenFlag}
	log.Debug().Strs("without_flags", crit.WithoutFlags).Msg("search criteria - without flags")

	if to := strings.TrimSpace(cl.cfg.SearchTo); to != "" {
		crit.Header = make(textproto.MIMEHeader)
		crit.Header.Set("To", to)
		log.Debug().Str("search_to", to).Msg("search criteria - filtering by To header")
	}

	uids, err := cl.c.Search(crit)
	if err != nil {
		return cl.handleFetchError(ctx, err, retryCount)
	}

	log.Debug().Int("count", len(uids)).Interface("uids", uids).Msg("found unseen message UIDs")

	if len(uids) == 0 {
		return nil, nil
	}

	// Try a simple fetch by sequence number instead of UID
	log.Debug().Msg("attempting sequence-based search as fallback")
	seqCrit := imap.NewSearchCriteria()
	seqCrit.WithoutFlags = []string{imap.SeenFlag}
	seqNums, err := cl.c.Search(seqCrit)
	if err == nil {
		log.Debug().Interface("sequence_numbers", seqNums).Msg("found by sequence numbers")
	}

	// Try using sequence numbers instead of UIDs
	log.Debug().Msg("using sequence numbers instead of UIDs for fetch")
	seq := new(imap.SeqSet)
	for _, seqNum := range seqNums {
		seq.AddNum(seqNum)
	}

	log.Debug().Interface("sequence_numbers", seqNums).Msg("starting to fetch messages by sequence")

	// Fetch envelope, flags and body content
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, imap.FetchFlags, imap.FetchRFC822}

	log.Debug().Msg("attempting to fetch envelope, flags and body content")
	ch := make(chan *imap.Message, maxChannelBuffer)

	fetchErr := make(chan error, 1)
	go func() {
		messageCount := 0
		err := cl.c.Fetch(seq, items, ch) // Use Fetch instead of UidFetch
		fetchErr <- err
		if err != nil {
			log.Debug().Err(err).Int("messages_received", messageCount).Msg("Fetch completed with error")
		} else {
			log.Debug().Int("messages_received", messageCount).Msg("Fetch completed successfully")
		}
	}()
	var out []Email
	fetchCompleted := false

	for {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case err := <-fetchErr:
			fetchCompleted = true
			if err != nil {
				log.Debug().Err(err).Msg("UidFetch failed")
				return cl.handleFetchError(ctx, err, retryCount)
			}
			log.Debug().Msg("UidFetch completed, waiting for remaining messages")
		case msg, ok := <-ch:
			if !ok {
				log.Debug().Bool("fetch_completed", fetchCompleted).Int("messages_count", len(out)).Msg("message channel closed")
				return out, nil
			}
			if msg == nil {
				log.Debug().Msg("received nil message, skipping")
				continue
			}
			if msg.Envelope == nil {
				log.Debug().Uint32("uid", msg.Uid).Msg("received message with nil envelope, skipping")
				continue
			}

			log.Debug().Uint32("uid", msg.Uid).Str("subject", msg.Envelope.Subject).Strs("flags", msg.Flags).Msg("processing fetched message")

			fromName, fromAddr := "", ""
			if len(msg.Envelope.From) > 0 {
				f := msg.Envelope.From[0]
				fromAddr = fmt.Sprintf("%s@%s", f.MailboxName, f.HostName)
				fromName = strings.TrimSpace(f.PersonalName)
				if fromName == "" {
					fromName = fromAddr
				}
				log.Debug().Str("from_addr", fromAddr).Str("from_name", fromName).Msg("extracted sender info")
			} else {
				log.Debug().Msg("no sender found in envelope")
			}
			body := ""
			var attachments []Attachment

			// Get body content from the message we already fetched
			if r := msg.GetBody(&imap.BodySectionName{}); r != nil {
				log.Debug().Uint32("uid", msg.Uid).Str("subject", msg.Envelope.Subject).Msg("parsing message body content")
				body, attachments = parseEmailContent(r)
				log.Debug().Uint32("uid", msg.Uid).Int("body_length", len(body)).Int("attachments_count", len(attachments)).Msg("body content parsed")

				// Log attachment details
				for i, att := range attachments {
					log.Debug().Uint32("uid", msg.Uid).Int("attachment_index", i).Str("filename", att.Filename).Str("content_type", att.ContentType).Int64("size", att.Size).Msg("parsed attachment")
				}

				if len(body) == 0 {
					log.Warn().Uint32("uid", msg.Uid).Str("subject", msg.Envelope.Subject).Msg("email body is empty after parsing")
				}
			} else {
				log.Debug().Uint32("uid", msg.Uid).Msg("no body content found in message")
			}

			email := Email{
				ID:          cl.mailbox + "-" + itoaU(msg.Uid),
				UID:         msg.Uid,
				Subject:     msg.Envelope.Subject,
				FromName:    fromName,
				FromEmail:   fromAddr,
				Body:        body,
				Attachments: attachments,
			}

			log.Debug().Str("email_id", email.ID).Str("from", fromAddr).Str("subject", msg.Envelope.Subject).Msg("email processed successfully")
			out = append(out, email)
		}
	}
}

// wrapper around fetchUnseenWithRetry to handle connection errors and retry
func (cl *Client) handleFetchError(ctx context.Context, err error, retryCount int) ([]Email, error) {
	if !isConnectionError(err) {
		// Not a connection error, return as-is
		return nil, err
	}

	if retryCount >= maxRetryAttempts {
		log.Error().Err(err).Int("retry_count", retryCount).Msg("max retry attempts reached for IMAP fetch")
		return nil, err
	}

	log.Warn().Err(err).Int("retry_count", retryCount).Msg("IMAP connection error detected, attempting reconnect")

	// Try to reconnect
	if reconnectErr := cl.reconnect(); reconnectErr != nil {
		log.Error().Err(reconnectErr).Msg("failed to reconnect to IMAP server")
		return nil, reconnectErr
	}

	// Retry the operation
	return cl.fetchUnseenWithRetry(ctx, retryCount+1)
}

// MarkSeen marks an email as seen and optionally adds a custom processed keyword.
func (cl *Client) MarkSeen(ctx context.Context, uid uint32) error {
	return cl.markSeenWithRetry(ctx, uid, 0)
}

// markSeenWithRetry implements MarkSeen with automatic reconnection
func (cl *Client) markSeenWithRetry(ctx context.Context, uid uint32, retryCount int) error {
	// Check if context was cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	seq := new(imap.SeqSet)
	seq.AddNum(uid)
	flags := []interface{}{imap.SeenFlag}
	if kw := strings.TrimSpace(cl.cfg.ProcessedKeyword); kw != "" {
		flags = append(flags, kw)
	}

	err := cl.c.UidStore(seq, imap.AddFlags, flags, nil)
	if err != nil && isConnectionError(err) {
		if retryCount >= maxRetryAttempts {
			log.Error().Err(err).Uint32("uid", uid).Int("retry_count", retryCount).Msg("max retry attempts reached for IMAP MarkSeen")
			return err
		}

		log.Warn().Err(err).Uint32("uid", uid).Int("retry_count", retryCount).Msg("IMAP connection error in MarkSeen, attempting reconnect")

		if reconnectErr := cl.reconnect(); reconnectErr != nil {
			log.Error().Err(reconnectErr).Msg("failed to reconnect to IMAP server in MarkSeen")
			return reconnectErr
		}

		// Retry the operation
		return cl.markSeenWithRetry(ctx, uid, retryCount+1)
	}

	return err
}

// --- helpers ---

func htmlToText(s string) string {
	// Handle common HTML entities first
	s = decodeHTMLEntities(s)

	// Simple HTML tag removal with basic line break handling
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "<div>", "")
	s = strings.ReplaceAll(s, "</div>", "\n")
	s = strings.ReplaceAll(s, "<li>", "- ")
	s = strings.ReplaceAll(s, "</li>", "\n")

	// Strip remaining tags
	var out strings.Builder
	intag := false
	for _, r := range s {
		if r == '<' {
			intag = true
			continue
		}
		if r == '>' {
			intag = false
			continue
		}
		if !intag {
			out.WriteRune(r)
		}
	}

	// Clean up multiple newlines and spaces
	result := out.String()
	result = strings.ReplaceAll(result, "\r\n", "\n")
	result = strings.ReplaceAll(result, "\r", "\n")

	// Remove excessive blank lines (more than 2 consecutive newlines)
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(result)
}

// decodeHTMLEntities decodes common HTML entities to their character equivalents
func decodeHTMLEntities(s string) string {
	// Named entities
	entities := map[string]string{
		"&nbsp;":   " ",
		"&amp;":    "&",
		"&lt;":     "<",
		"&gt;":     ">",
		"&quot;":   "\"",
		"&apos;":   "'",
		"&copy;":   "©",
		"&reg;":    "®",
		"&euro;":   "€",
		"&pound;":  "£",
		"&yen;":    "¥",
		"&cent;":   "¢",
		"&deg;":    "°",
		"&mdash;":  "—",
		"&ndash;":  "–",
		"&hellip;": "…",
		"&laquo;":  "«",
		"&raquo;":  "»",
		"&bull;":   "•",
		"&trade;":  "™",
		// Czech-specific entities
		"&aacute;": "á",
		"&Aacute;": "Á",
		"&eacute;": "é",
		"&Eacute;": "É",
		"&iacute;": "í",
		"&Iacute;": "Í",
		"&oacute;": "ó",
		"&Oacute;": "Ó",
		"&uacute;": "ú",
		"&Uacute;": "Ú",
		"&yacute;": "ý",
		"&Yacute;": "Ý",
		"&ccaron;": "č",
		"&Ccaron;": "Č",
		"&dcaron;": "ď",
		"&Dcaron;": "Ď",
		"&ecaron;": "ě",
		"&Ecaron;": "Ě",
		"&ncaron;": "ň",
		"&Ncaron;": "Ň",
		"&rcaron;": "ř",
		"&Rcaron;": "Ř",
		"&scaron;": "š",
		"&Scaron;": "Š",
		"&tcaron;": "ť",
		"&Tcaron;": "Ť",
		"&uring;":  "ů",
		"&Uring;":  "Ů",
		"&zcaron;": "ž",
		"&Zcaron;": "Ž",
	}

	for entity, char := range entities {
		s = strings.ReplaceAll(s, entity, char)
	}

	// Handle numeric entities (&#123; or &#x7B;)
	s = decodeNumericEntities(s)

	return s
}

// decodeNumericEntities decodes numeric HTML entities like &#123; or &#x7B;
func decodeNumericEntities(s string) string {
	// Decimal entities: &#123;
	for {
		start := strings.Index(s, "&#")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], ";")
		if end == -1 {
			break
		}
		end += start

		entity := s[start : end+1]
		numStr := s[start+2 : end]

		var num int64
		var err error

		if strings.HasPrefix(numStr, "x") || strings.HasPrefix(numStr, "X") {
			// Hexadecimal: &#x7B;
			num, err = strconv.ParseInt(numStr[1:], 16, 32)
		} else {
			// Decimal: &#123;
			num, err = strconv.ParseInt(numStr, 10, 32)
		}

		if err == nil && num > 0 && num < 0x10FFFF {
			s = strings.Replace(s, entity, string(rune(num)), 1)
		} else {
			// Invalid entity, skip it by moving past
			break
		}
	}

	return s
}

// ExtractTicketID extracts a ticket ID from an email subject line using the expected prefix pattern.
func ExtractTicketID(subject, expectedPrefix string) (int, bool) {
	// [PREFIX-#123]
	re := regexp.MustCompile(`\[\s*([A-Za-z0-9_-]+)\s*-\s*#(\d+)\s*\]`)
	m := re.FindStringSubmatch(subject)
	if len(m) != maxRegexGroups {
		return 0, false
	}
	if !strings.EqualFold(m[1], expectedPrefix) {
		return 0, false
	}
	id, _ := strconv.Atoi(m[2])
	return id, id > 0
}

// CleanBody removes quoted text and previous messages from email body text.
func CleanBody(b string) string {
	lines := strings.Split(b, "\n")
	var out []string

	// Patterns that indicate the start of quoted/forwarded content
	breakPatterns := []string{
		"original message",
		"původní zpráva",
		"---------- forwarded message",
		"---------- přeposlaná zpráva",
		"on wrote:",
		"napsal:",
		"from:",
		"od:",
		"sent:",
		"odesláno:",
		"-------- original message --------",
		"-------- původní zpráva --------",
	}

	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		trimLower := strings.ToLower(trim)

		// Skip quoted lines
		if strings.HasPrefix(trim, ">") {
			continue
		}

		// Check for break patterns
		shouldBreak := false
		for _, pattern := range breakPatterns {
			if strings.Contains(trimLower, pattern) {
				shouldBreak = true
				break
			}
		}
		if shouldBreak {
			break
		}

		out = append(out, ln)
	}

	s := strings.TrimSpace(strings.Join(out, "\n"))

	// If result is empty and original had content, return original content
	if s == "" && strings.TrimSpace(b) != "" {
		log.Debug().Msg("CleanBody resulted in empty string, returning original content")
		return strings.TrimSpace(b)
	}

	return s
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
func itoaU(v uint32) string {
	return fmt.Sprintf("%d", v)
}

// decodeTextContent decodes text content based on Content-Transfer-Encoding and charset
func decodeTextContent(data []byte, encoding string, charset string) string {
	// First decode the transfer encoding
	var decoded []byte
	switch strings.ToLower(encoding) {
	case "quoted-printable":
		reader := quotedprintable.NewReader(strings.NewReader(string(data)))
		decoded, _ = io.ReadAll(reader)
	case "base64":
		decoded, _ = base64.StdEncoding.DecodeString(string(data))
	default:
		decoded = data
	}

	// Convert from charset to UTF-8 if needed
	text := decodeCharset(decoded, charset)

	return strings.TrimSpace(text)
}

// decodeCharset converts text from various charsets to UTF-8
func decodeCharset(data []byte, charset string) string {
	if charset == "" {
		return string(data)
	}

	charset = strings.ToLower(strings.TrimSpace(charset))

	// Map of charset aliases
	charsetMap := map[string][]byte{
		"iso-8859-1":   nil, // Latin-1
		"iso-8859-2":   nil, // Latin-2 (Central European)
		"windows-1250": nil, // Windows Central European
		"windows-1252": nil, // Windows Western European
		"cp1250":       nil, // Same as windows-1250
		"cp1252":       nil, // Same as windows-1252
	}

	// Check if we need charset conversion
	_, needsConversion := charsetMap[charset]
	if !needsConversion && charset != "utf-8" && charset != "us-ascii" {
		// Unknown charset, try as-is
		log.Debug().Str("charset", charset).Msg("unknown charset, using as-is")
	}

	// For ISO-8859-2 and Windows-1250 (Czech/Central European)
	if charset == "iso-8859-2" || charset == "windows-1250" || charset == "cp1250" {
		return decodeISO8859_2(data)
	}

	// For ISO-8859-1 and Windows-1252 (Western European)
	if charset == "iso-8859-1" || charset == "windows-1252" || charset == "cp1252" {
		return decodeWindows1252(data)
	}

	return string(data)
}

// decodeISO8859_2 decodes ISO-8859-2 (Latin-2, Central European) to UTF-8
func decodeISO8859_2(data []byte) string {
	// ISO-8859-2 to UTF-8 mapping for values 0x80-0xFF
	iso8859_2 := map[byte]rune{
		0xA1: 'Ą', 0xA2: '˘', 0xA3: 'Ł', 0xA4: '¤', 0xA5: 'Ľ', 0xA6: 'Ś', 0xA7: '§',
		0xA8: '¨', 0xA9: 'Š', 0xAA: 'Ş', 0xAB: 'Ť', 0xAC: 'Ź', 0xAD: '\u00AD', 0xAE: 'Ž', 0xAF: 'Ż',
		0xB0: '°', 0xB1: 'ą', 0xB2: '˛', 0xB3: 'ł', 0xB4: '´', 0xB5: 'ľ', 0xB6: 'ś', 0xB7: 'ˇ',
		0xB8: '¸', 0xB9: 'š', 0xBA: 'ş', 0xBB: 'ť', 0xBC: 'ź', 0xBD: '˝', 0xBE: 'ž', 0xBF: 'ż',
		0xC0: 'Ŕ', 0xC1: 'Á', 0xC2: 'Â', 0xC3: 'Ă', 0xC4: 'Ä', 0xC5: 'Ĺ', 0xC6: 'Ć', 0xC7: 'Ç',
		0xC8: 'Č', 0xC9: 'É', 0xCA: 'Ę', 0xCB: 'Ë', 0xCC: 'Ě', 0xCD: 'Í', 0xCE: 'Î', 0xCF: 'Ď',
		0xD0: 'Đ', 0xD1: 'Ń', 0xD2: 'Ň', 0xD3: 'Ó', 0xD4: 'Ô', 0xD5: 'Ő', 0xD6: 'Ö', 0xD7: '×',
		0xD8: 'Ř', 0xD9: 'Ů', 0xDA: 'Ú', 0xDB: 'Ű', 0xDC: 'Ü', 0xDD: 'Ý', 0xDE: 'Ţ', 0xDF: 'ß',
		0xE0: 'ŕ', 0xE1: 'á', 0xE2: 'â', 0xE3: 'ă', 0xE4: 'ä', 0xE5: 'ĺ', 0xE6: 'ć', 0xE7: 'ç',
		0xE8: 'č', 0xE9: 'é', 0xEA: 'ę', 0xEB: 'ë', 0xEC: 'ě', 0xED: 'í', 0xEE: 'î', 0xEF: 'ď',
		0xF0: 'đ', 0xF1: 'ń', 0xF2: 'ň', 0xF3: 'ó', 0xF4: 'ô', 0xF5: 'ő', 0xF6: 'ö', 0xF7: '÷',
		0xF8: 'ř', 0xF9: 'ů', 0xFA: 'ú', 0xFB: 'ű', 0xFC: 'ü', 0xFD: 'ý', 0xFE: 'ţ', 0xFF: '˙',
	}

	var result strings.Builder
	for _, b := range data {
		if b < asciiUpperBound {
			result.WriteByte(b)
		} else if r, ok := iso8859_2[b]; ok {
			result.WriteRune(r)
		} else {
			result.WriteByte(b)
		}
	}
	return result.String()
}

// decodeWindows1252 decodes Windows-1252 (Western European) to UTF-8
func decodeWindows1252(data []byte) string {
	// Windows-1252 specific characters (0x80-0x9F range differs from ISO-8859-1)
	windows1252 := map[byte]rune{
		0x80: '\u20AC', 0x82: '\u201A', 0x83: '\u0192', 0x84: '\u201E', 0x85: '\u2026', 0x86: '\u2020', 0x87: '\u2021',
		0x88: '\u02C6', 0x89: '\u2030', 0x8A: '\u0160', 0x8B: '\u2039', 0x8C: '\u0152', 0x8E: '\u017D',
		0x91: '\u2018', 0x92: '\u2019', 0x93: '\u201C', 0x94: '\u201D', 0x95: '\u2022', 0x96: '\u2013', 0x97: '\u2014',
		0x98: '\u02DC', 0x99: '\u2122', 0x9A: '\u0161', 0x9B: '\u203A', 0x9C: '\u0153', 0x9E: '\u017E', 0x9F: '\u0178',
	}

	var result strings.Builder
	for _, b := range data {
		if b < asciiUpperBound {
			result.WriteByte(b)
		} else if r, ok := windows1252[b]; ok {
			result.WriteRune(r)
		} else if b >= latin1ExtendedStart {
			// ISO-8859-1 range (same as Unicode for 0xA0-0xFF)
			result.WriteRune(rune(b))
		} else {
			result.WriteByte(b)
		}
	}
	return result.String()
}

// extractBodyFromRawEmail extracts body content from raw email text by skipping headers
func extractBodyFromRawEmail(rawEmail string) string {
	lines := strings.Split(rawEmail, "\n")
	inBody := false
	var bodyLines []string

	for _, line := range lines {
		if !inBody {
			// Look for empty line that separates headers from body
			if strings.TrimSpace(line) == "" {
				inBody = true
				continue
			}
		} else {
			// We're in the body section
			bodyLines = append(bodyLines, line)
		}
	}

	body := strings.Join(bodyLines, "\n")
	body = strings.TrimSpace(body)

	// Try to extract text from multipart content
	if strings.Contains(body, "Content-Type: text/plain") {
		extractedText := extractTextFromMultipart(body)
		if extractedText != "" {
			body = extractedText
		}
	}

	log.Debug().Int("body_length", len(body)).Msg("extracted body from raw email")
	return body
}

// extractTextFromMultipart extracts text content from multipart email body
func extractTextFromMultipart(multipartBody string) string {
	lines := strings.Split(multipartBody, "\n")
	var textContent []string
	inTextSection := false
	isQuotedPrintable := false

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		// Check for text/plain content type
		if strings.Contains(line, "Content-Type: text/plain") {
			inTextSection = true
			continue
		}

		// Check for quoted-printable encoding
		if strings.Contains(line, "Content-Transfer-Encoding: quoted-printable") {
			isQuotedPrintable = true
			continue
		}

		// Check for boundary or next content type (end of text section)
		if inTextSection && (strings.HasPrefix(line, "--") || strings.Contains(line, "Content-Type:")) {
			break
		}

		// Skip empty lines after headers
		if inTextSection && strings.TrimSpace(line) == "" && len(textContent) == 0 {
			continue
		}

		// Collect text content
		if inTextSection {
			textContent = append(textContent, line)
		}
	}

	if len(textContent) == 0 {
		return ""
	}

	text := strings.Join(textContent, "\n")
	text = strings.TrimSpace(text)

	// Decode quoted-printable if needed
	if isQuotedPrintable {
		text = decodeTextContent([]byte(text), "quoted-printable", "")
	}

	log.Debug().Int("extracted_multipart_length", len(text)).Bool("was_quoted_printable", isQuotedPrintable).Msg("extracted text from multipart")
	return text
}

// parseEmailContent parses email content and extracts both body text and attachments
//
//nolint:gocyclo,gocritic // Email parsing requires extensive conditional logic and complex if-else chains
func parseEmailContent(r io.Reader) (string, []Attachment) {
	log.Debug().Msg("parsing email content for body and attachments")

	// Read all data first so we can reuse if needed
	data, err := io.ReadAll(r)
	if err != nil {
		log.Debug().Err(err).Msg("failed to read email data")
		return "", nil
	}

	log.Debug().Int("data_size", len(data)).Msg("read email data")

	// Log first few lines of raw email for debugging
	previewLen := 500
	if len(data) < previewLen {
		previewLen = len(data)
	}
	lines := strings.Split(string(data[:previewLen]), "\n")
	for i, line := range lines {
		if i >= maxHeaderLines {
			break
		}
		log.Debug().Int("line", i).Str("content", line).Msg("email header line")
	}

	mr, err := message.Read(strings.NewReader(string(data)))
	if err != nil {
		// Simple email without MIME - try to extract body manually
		log.Debug().Err(err).Msg("simple email without MIME structure, trying to extract body manually")
		body := extractBodyFromRawEmail(string(data))
		return body, nil
	}

	mt, _, _ := mr.Header.ContentType()
	log.Debug().Str("content_type", mt).Msg("email content type detected")

	if !strings.HasPrefix(mt, "multipart/") {
		// Simple content type
		if strings.HasPrefix(mt, "text/plain") {
			b, _ := io.ReadAll(mr.Body)
			return string(b), nil
		}
		if strings.HasPrefix(mt, "text/html") {
			b, _ := io.ReadAll(mr.Body)
			return htmlToText(string(b)), nil
		}
		return "", nil
	}

	// Multipart message - parse all parts
	var bodyText string
	var attachments []Attachment

	// First pass: collect text parts and attachments
	parts := make([]struct {
		header message.Header
		body   []byte
	}, 0)

	mpr := mr.MultipartReader()
	for {
		part, err := mpr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		body, _ := io.ReadAll(part.Body)
		parts = append(parts, struct {
			header message.Header
			body   []byte
		}{
			header: part.Header,
			body:   body,
		})
	}

	// Process collected parts
	log.Debug().Int("total_parts", len(parts)).Msg("processing multipart email parts")

	// If no parts found, fall back to raw email parsing
	if len(parts) == 0 {
		log.Debug().Msg("no multipart parts found, falling back to raw email parsing")
		body := extractBodyFromRawEmail(string(data))
		return body, nil
	}

	for i, part := range parts {
		ct, params, _ := part.header.ContentType()
		disposition, dispParams := getContentDisposition(part.header)

		log.Debug().Int("part_index", i).Str("content_type", ct).Str("disposition", disposition).Int("body_size", len(part.body)).Msg("processing email part")

		// Check if this is an attachment
		if disposition == attachmentDisposition || disposition == inlineDisposition {
			filename := dispParams["filename"]
			if filename == "" {
				filename = params["name"]
			}
			if filename == "" {
				// Generate filename based on content type
				if ext := getExtensionForContentType(ct); ext != "" {
					filename = "attachment" + ext
				} else {
					filename = "attachment.bin"
				}
			}

			// Decode content if needed
			data := part.body
			if encoding := part.header.Get("Content-Transfer-Encoding"); encoding != "" {
				if strings.ToLower(encoding) == base64Encoding {
					decoded, err := base64.StdEncoding.DecodeString(string(data))
					if err == nil {
						data = decoded
					}
				}
			}

			attachments = append(attachments, Attachment{
				Filename:    filename,
				ContentType: ct,
				Size:        int64(len(data)),
				Data:        data,
			})
			log.Debug().Str("filename", filename).Str("content_type", ct).Int("size", len(data)).Msg("added attachment")
		} else if strings.HasPrefix(ct, "text/") {
			// Text content for body
			log.Debug().Str("text_content_type", ct).Int("body_size", len(part.body)).Bool("body_empty", bodyText == "").Msg("found text content")
			if bodyText == "" { // Prefer first text part
				if strings.HasPrefix(ct, "text/plain") {
					bodyText = string(part.body)
					log.Debug().Int("extracted_text_length", len(bodyText)).Msg("extracted plain text")
				} else if strings.HasPrefix(ct, "text/html") {
					bodyText = htmlToText(string(part.body))
					log.Debug().Int("extracted_text_length", len(bodyText)).Msg("extracted HTML text")
				}
			}
		} else if strings.HasPrefix(ct, "multipart/") {
			// Nested multipart - create proper MIME message for parsing
			log.Debug().Str("nested_multipart_type", ct).Msg("processing nested multipart")

			// Create complete MIME message with Content-Type header
			contentType := part.header.Get("Content-Type")
			if contentType == "" {
				contentType = ct
			}

			// Handle charset issues in the multipart body
			multipartBody := string(part.body)
			multipartBody = strings.ReplaceAll(multipartBody, "charset=\"iso-8859-2\"", "charset=\"utf-8\"")
			multipartBody = strings.ReplaceAll(multipartBody, "charset=iso-8859-2", "charset=utf-8")
			multipartBody = strings.ReplaceAll(multipartBody, "charset=\"windows-1252\"", "charset=\"utf-8\"")
			multipartBody = strings.ReplaceAll(multipartBody, "charset=windows-1252", "charset=utf-8")

			mimeMessage := "Content-Type: " + contentType + "\r\n\r\n" + multipartBody

			// Try our multipart parser first, then fallback to message.Read
			if bodyText == "" {
				extractedText := extractTextFromMultipart(multipartBody)
				if extractedText != "" {
					bodyText = extractedText
					log.Debug().Int("direct_extracted_length", len(bodyText)).Msg("extracted text directly using multipart parser")
					continue
				}
			}

			// Parse as complete message
			nestedMsg, err := message.Read(strings.NewReader(mimeMessage))
			if err != nil {
				log.Debug().Err(err).Msg("failed to parse nested multipart, skipping")
				continue
			}

			if nestedMpr := nestedMsg.MultipartReader(); nestedMpr != nil {
				// Parse nested parts
				for {
					nestedPart, err := nestedMpr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						log.Debug().Err(err).Msg("error reading nested part")
						break
					}

					nestedBody, err := io.ReadAll(nestedPart.Body)
					if err != nil {
						log.Debug().Err(err).Msg("error reading nested part body")
						continue
					}

					nestedCt, nestedParams, _ := nestedPart.Header.ContentType()
					nestedDisposition, nestedDispParams := getContentDisposition(nestedPart.Header)

					log.Debug().Str("nested_content_type", nestedCt).Str("nested_disposition", nestedDisposition).Int("nested_body_size", len(nestedBody)).Msg("processing nested part")

					// Check if nested part is attachment
					isNestedAttachment := false
					if nestedDisposition == attachmentDisposition {
						isNestedAttachment = true
						log.Debug().Str("reason", "nested_disposition_attachment").Msg("nested part is attachment")
					} else if nestedDisposition == inlineDisposition && !strings.HasPrefix(nestedCt, "text/") {
						isNestedAttachment = true
						log.Debug().Str("reason", "nested_disposition_inline_non_text").Msg("nested part is attachment")
					} else if nestedParams["name"] != "" && !strings.HasPrefix(nestedCt, "text/") {
						isNestedAttachment = true
						log.Debug().Str("reason", "nested_ct_name_non_text").Msg("nested part is attachment")
					} else if strings.HasPrefix(nestedCt, "image/") || strings.HasPrefix(nestedCt, "application/") {
						isNestedAttachment = true
						log.Debug().Str("reason", "nested_common_attachment_type").Msg("nested part is attachment")
					}

					if isNestedAttachment {
						nestedFilename := nestedDispParams["filename"]
						if nestedFilename == "" {
							nestedFilename = nestedParams["name"]
						}
						if nestedFilename == "" {
							if ext := getExtensionForContentType(nestedCt); ext != "" {
								nestedFilename = "nested_attachment" + ext
							} else {
								nestedFilename = "nested_attachment.bin"
							}
						}

						// Decode nested content if needed
						nestedData := nestedBody
						if nestedEncoding := nestedPart.Header.Get("Content-Transfer-Encoding"); nestedEncoding != "" {
							if strings.ToLower(nestedEncoding) == base64Encoding {
								decoded, err := base64.StdEncoding.DecodeString(string(nestedData))
								if err == nil {
									nestedData = decoded
								} else {
									log.Debug().Err(err).Str("filename", nestedFilename).Msg("failed to decode base64 nested attachment")
								}
							}
						}

						attachments = append(attachments, Attachment{
							Filename:    nestedFilename,
							ContentType: nestedCt,
							Size:        int64(len(nestedData)),
							Data:        nestedData,
						})
						log.Debug().Str("filename", nestedFilename).Str("content_type", nestedCt).Int("size", len(nestedData)).Msg("added nested attachment")
					} else if strings.HasPrefix(nestedCt, "text/") && bodyText == "" {
						// Text content for body
						nestedEncoding := nestedPart.Header.Get("Content-Transfer-Encoding")
						nestedCharset := nestedParams["charset"]

						if strings.HasPrefix(nestedCt, "text/plain") {
							bodyText = decodeTextContent(nestedBody, nestedEncoding, nestedCharset)
							log.Debug().Int("extracted_nested_text_length", len(bodyText)).Str("encoding", nestedEncoding).Str("charset", nestedCharset).Msg("extracted text from nested plain part")
						} else if strings.HasPrefix(nestedCt, "text/html") {
							decodedHTML := decodeTextContent(nestedBody, nestedEncoding, nestedCharset)
							bodyText = htmlToText(decodedHTML)
							log.Debug().Int("extracted_nested_text_length", len(bodyText)).Str("encoding", nestedEncoding).Str("charset", nestedCharset).Msg("extracted text from nested HTML part")
						}
					}
				}
			}
		} else {
			// Check if this might be an attachment based on content-type or disposition
			log.Debug().Str("content_type", ct).Str("disposition", disposition).Interface("disp_params", dispParams).Interface("ct_params", params).Int("body_size", len(part.body)).Msg("analyzing part for attachment detection")

			// Determine if this is an attachment
			isAttachment := false
			filename := ""

			switch disposition {
			case attachmentDisposition:
				isAttachment = true
				filename = dispParams["filename"]
				log.Debug().Str("reason", "disposition_attachment").Str("filename", filename).Msg("detected attachment")
			case inlineDisposition:
				// Inline can be attachment if it's not text
				if !strings.HasPrefix(ct, "text/") {
					isAttachment = true
					filename = dispParams["filename"]
					log.Debug().Str("reason", "disposition_inline_non_text").Str("filename", filename).Msg("detected attachment")
				}
			}

			// Also check content-type parameters for filename
			if !isAttachment && params["name"] != "" && !strings.HasPrefix(ct, "text/") {
				isAttachment = true
				filename = params["name"]
				log.Debug().Str("reason", "ct_name_non_text").Str("filename", filename).Msg("detected attachment")
			}

			// Check for common attachment content types even without explicit disposition
			if !isAttachment && (strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "application/")) {
				isAttachment = true
				filename = params["name"]
				if filename == "" {
					filename = dispParams["filename"]
				}
				log.Debug().Str("reason", "common_attachment_type").Str("content_type", ct).Str("filename", filename).Msg("detected attachment by content type")
			}

			// If no filename found, generate one based on content-type
			if isAttachment && filename == "" {
				if ext := getExtensionForContentType(ct); ext != "" {
					filename = "attachment" + ext
				} else {
					filename = "attachment.bin"
				}
				log.Debug().Str("generated_filename", filename).Msg("generated filename for attachment")
			}

			if isAttachment {
				// Decode content if needed
				data := part.body
				encoding := part.header.Get("Content-Transfer-Encoding")
				log.Debug().Str("encoding", encoding).Int("raw_size", len(data)).Msg("processing attachment encoding")

				if encoding != "" {
					if strings.ToLower(encoding) == base64Encoding {
						decoded, err := base64.StdEncoding.DecodeString(string(data))
						if err == nil {
							data = decoded
							log.Debug().Int("decoded_size", len(data)).Msg("successfully decoded base64 attachment")
						} else {
							log.Debug().Err(err).Str("filename", filename).Msg("failed to decode base64 attachment")
						}
					}
				}

				attachments = append(attachments, Attachment{
					Filename:    filename,
					ContentType: ct,
					Size:        int64(len(data)),
					Data:        data,
				})
				log.Debug().Str("filename", filename).Str("content_type", ct).Int("size", len(data)).Msg("added attachment")
			} else if strings.HasPrefix(ct, "text/") && bodyText == "" {
				// This is text content for body
				encoding := part.header.Get("Content-Transfer-Encoding")
				charset := params["charset"]

				if strings.HasPrefix(ct, "text/plain") {
					bodyText = decodeTextContent(part.body, encoding, charset)
					log.Debug().Int("extracted_text_length", len(bodyText)).Str("encoding", encoding).Str("charset", charset).Msg("extracted text from plain part")
				} else if strings.HasPrefix(ct, "text/html") {
					decodedHTML := decodeTextContent(part.body, encoding, charset)
					bodyText = htmlToText(decodedHTML)
					log.Debug().Int("extracted_text_length", len(bodyText)).Str("encoding", encoding).Str("charset", charset).Msg("extracted text from HTML part")
				}
			} else {
				log.Debug().Str("content_type", ct).Str("disposition", disposition).Bool("is_attachment", isAttachment).Msg("part not processed as attachment or text")
			}
		}
	}

	// If we still don't have body text, try parsing the whole message as text
	if bodyText == "" && len(attachments) == 0 {
		log.Debug().Msg("no body text or attachments found, trying to parse message as plain text")
		bodyText = string(data)
	}

	log.Debug().Int("final_body_length", len(bodyText)).Int("attachments_count", len(attachments)).Msg("parseEmailContent completed")
	if bodyText == "" {
		log.Warn().Msg("email body text is empty after parsing")
	}
	return bodyText, attachments
}

// getContentDisposition parses Content-Disposition header
func getContentDisposition(header message.Header) (string, map[string]string) {
	cd := header.Get("Content-Disposition")
	if cd == "" {
		return "", nil
	}

	mediaType, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return "", nil
	}

	return mediaType, params
}

// getExtensionForContentType returns file extension for common content types
func getExtensionForContentType(contentType string) string {
	// Use predefined mapping to ensure consistent results across systems
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "application/pdf":
		return ".pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/msword":
		return ".doc"
	case "application/vnd.ms-excel":
		return ".xls"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "application/zip":
		return ".zip"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/tiff":
		return ".tiff"
	}

	// Fallback for partial matches
	switch {
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "pdf"):
		return ".pdf"
	case strings.Contains(contentType, "word"):
		return ".docx"
	case strings.Contains(contentType, "excel"):
		return ".xlsx"
	case strings.Contains(contentType, "image/"):
		return ".bin" // generic image if we don't know specific format
	case strings.Contains(contentType, "text/"):
		return ".txt"
	}
	return ""
}
