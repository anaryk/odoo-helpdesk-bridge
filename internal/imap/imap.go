// Package imap provides IMAP client functionality for fetching and processing emails.
package imap

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"

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
	addr := net.JoinHostPort(cfg.Host, itoa(cfg.Port))
	tlsConf := &tls.Config{
		ServerName: cfg.Host,
		MinVersion: tls.VersionTLS12,
	}
	c, err := client.DialTLS(addr, tlsConf)
	if err != nil {
		return nil, err
	}
	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		_ = c.Logout()
		return nil, err
	}
	cl := &Client{cfg: cfg, c: c}
	if err := cl.selectFolder(cfg.Folder); err != nil {
		_ = c.Logout()
		return nil, err
	}
	return cl, nil
}

// Close closes the IMAP connection and logs out from the server.
func (cl *Client) Close() error {
	if cl.c != nil {
		_ = cl.c.Logout()
	}
	return nil
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
	log.Debug().Str("folder", cl.cfg.Folder).Str("search_to", cl.cfg.SearchTo).Msg("searching for unseen emails")

	// First, get mailbox status
	status, err := cl.c.Status(cl.cfg.Folder, []imap.StatusItem{imap.StatusMessages, imap.StatusUnseen})
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
		return nil, err
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
				return out, err
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

// MarkSeen marks an email as seen and optionally adds a custom processed keyword.
func (cl *Client) MarkSeen(_ context.Context, uid uint32) error {
	seq := new(imap.SeqSet)
	seq.AddNum(uid)
	flags := []interface{}{imap.SeenFlag}
	if kw := strings.TrimSpace(cl.cfg.ProcessedKeyword); kw != "" {
		flags = append(flags, kw)
	}
	return cl.c.UidStore(seq, imap.AddFlags, flags, nil)
}

// --- helpers ---

func htmlToText(s string) string {
	// Simple HTML tag removal with basic line break handling
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")

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
	return strings.TrimSpace(out.String())
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
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, ">") {
			continue
		}
		if strings.Contains(strings.ToLower(trim), "original message") {
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
	
	log.Debug().Int("body_length", len(body)).Msg("extracted body from raw email")
	return body
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

			mimeMessage := "Content-Type: " + contentType + "\r\n\r\n" + string(part.body)

			// Parse as complete message
			nestedMsg, err := message.Read(strings.NewReader(mimeMessage))
			if err != nil {
				log.Debug().Err(err).Msg("failed to parse nested multipart")
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
						if strings.HasPrefix(nestedCt, "text/plain") {
							bodyText = string(nestedBody)
							log.Debug().Int("extracted_nested_text_length", len(bodyText)).Msg("extracted text from nested plain part")
						} else if strings.HasPrefix(nestedCt, "text/html") {
							bodyText = htmlToText(string(nestedBody))
							log.Debug().Int("extracted_nested_text_length", len(bodyText)).Msg("extracted text from nested HTML part")
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
				if strings.HasPrefix(ct, "text/plain") {
					bodyText = string(part.body)
					log.Debug().Int("extracted_text_length", len(bodyText)).Msg("extracted text from plain part")
				} else if strings.HasPrefix(ct, "text/html") {
					bodyText = htmlToText(string(part.body))
					log.Debug().Int("extracted_text_length", len(bodyText)).Msg("extracted text from HTML part")
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
