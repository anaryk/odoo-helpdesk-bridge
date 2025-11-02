# Copilot Instructions for odoo-helpdesk-bridge

## Project Overview
- This project is a Go service that bridges IMAP email, Odoo Helpdesk, and Slack for automated ticket processing and notifications with SLA monitoring.
- Main entrypoint: `cmd/helpdesk-bridge/main.go`.
- Core logic is organized in `internal/` by integration domain: `imap/`, `odoo/`, `slack/`, `mailer/`, `templ/`, `state/`, `sla/`, and `config/`.
- Email templates are in `templates/` (Go text/template format).
- Comprehensive test suite with 95%+ coverage in `*_test.go` files.

## Architecture & Data Flow
- **IMAP**: Fetches unseen emails, parses, and extracts ticket info (`internal/imap/imap.go`).
- **Odoo**: Handles ticket creation, updates, and queries (`internal/odoo/odoo.go`).
- **Slack**: Sends threaded notifications with @channel mentions (`internal/slack/slack.go`).
- **Mailer**: Sends outgoing emails (`internal/mailer/mailer.go`).
- **State**: Tracks processed tickets/messages, Slack threads, and SLA states (`internal/state/state.go`).
- **SLA**: Monitors ticket response/resolution times and sends violations (`internal/sla/sla.go`).
- **Templates**: Used for email/Slack message formatting.
- **Config**: Loads YAML config with SLA and Slack threading settings.

## Key Patterns & Conventions
- Each integration is encapsulated in its own package under `internal/` with comprehensive test coverage.
- Slack threading: Store message info in state DB, use parent message for threading replies.
- SLA tracking: Initialize on ticket creation, check violations during polling cycles.
- Email/ticket IDs extracted using regex and normalized (see `ExtractTicketID` in `imap.go`).
- State management uses BBolt for persistence with JSON serialization for complex types.
- Error handling: Log errors but continue processing other items (don't fail entire batch).
- Configuration supports both webhook and Bot API for Slack (Bot API required for threading).

## Developer Workflows
- **Build**: `make build` or `go build ./cmd/helpdesk-bridge`
- **Test**: `make test` (runs tests with coverage) or `go test ./...`
- **Lint**: `make lint` (runs staticcheck, golangci-lint, go vet)
- **Security**: `make security` (runs govulncheck)
- **Docker**: `make docker-build` and `make docker-run`
- **CI Pipeline**: `make ci` (runs full CI pipeline locally)
- **Development**: `make run-dev` (builds and runs with config.yaml)

## Testing Strategy
- Unit tests for all major components with mocked dependencies
- State persistence tests using temporary databases
- HTTP mocking for Slack API tests
- Configuration validation tests with valid/invalid YAML
- SLA timing logic tests with time manipulation
- Email parsing tests with various formats (HTML, plain text, quoted content)

## External Dependencies
- IMAP: `github.com/emersion/go-imap` and `github.com/emersion/go-message`
- Odoo: HTTP/JSON-RPC calls to Odoo API
- Slack: Both webhook and Bot API (`chat.postMessage`) for threading
- State: `go.etcd.io/bbolt` for embedded database
- Config: `gopkg.in/yaml.v3` for YAML parsing

## CI/CD Pipeline
- **GitHub Actions**: `.github/workflows/ci-cd.yml`
- **Test Phase**: Unit tests, coverage, linting, security scans
- **Build Phase**: Multi-platform Docker builds pushed to GHCR
- **Release Phase**: Creates GitHub releases with binaries and changelog
- **Security**: Automated vulnerability scanning with govulncheck

## Configuration Examples
```yaml
app:
  sla:
    start_time_hours: 4      # SLA for starting work
    resolution_time_hours: 24 # SLA for completion
slack:
  bot_token: "xoxb-..."      # Required for threading
  channel_id: "C1234567890"  # Required for threading
```

## Project-Specific Advice
- Always use `slaHandler.InitializeTask()` when creating new tickets
- Store Slack message info using `state.StoreSlackMessage()` for threading
- SLA violations automatically add labels to Odoo and notify Slack threads
- Use `make ci` before committing to run full validation pipeline
- Follow the existing test patterns when adding new components
- State DB schema changes require migration logic in `state.go`
- Slack Bot API requires specific scopes: `chat:write`, `chat:write.public`

## Docker & Deployment
- Multi-stage Dockerfile optimized for size and security
- Runs as non-root user with health checks
- Supports both docker-compose and Kubernetes deployment
- State persistence via mounted volumes at `/app/data`
- Configuration via mounted file at `/app/config/config.yaml`

---
For questions about threading, SLA handling, or test patterns, review the relevant `internal/` package tests.
