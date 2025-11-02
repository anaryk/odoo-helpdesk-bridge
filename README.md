# Odoo Helpdesk Bridge

[![CI/CD Pipeline](https://github.com/anaryk/odoo-helpdesk-bridge/actions/workflows/ci-cd.yml/badge.svg)](https://github.com/anaryk/odoo-helpdesk-bridge/actions/workflows/ci-cd.yml)
[![codecov](https://codecov.io/gh/anaryk/odoo-helpdesk-bridge/branch/main/graph/badge.svg)](https://codecov.io/gh/anaryk/odoo-helpdesk-bridge)
[![Go Report Card](https://goreportcard.com/badge/github.com/anaryk/odoo-helpdesk-bridge)](https://goreportcard.com/report/github.com/anaryk/odoo-helpdesk-bridge)
[![Docker Image](https://ghcr-badge.herokuapp.com/anaryk/odoo-helpdesk-bridge/size)](https://github.com/anaryk/odoo-helpdesk-bridge/pkgs/container/odoo-helpdesk-bridge)

A Go service that bridges IMAP email, Odoo Helpdesk, and Slack for automated ticket processing and notifications with SLA monitoring.

## Features

- 📧 **Email Integration**: Monitors IMAP inbox for new emails and replies
- 🎫 **Ticket Management**: Automatically creates and updates Odoo helpdesk tickets
- 💬 **Slack Notifications**: Sends threaded notifications with @channel mentions
- ⏰ **SLA Monitoring**: Tracks ticket response and resolution times
- 🔄 **Thread Support**: Maintains conversation context in Slack threads
- 🏷️ **Auto Labeling**: Adds SLA violation labels to Odoo tickets
- 📊 **State Management**: Persistent tracking to avoid duplicate processing

## Architecture

```
┌─────────────┐    ┌──────────────────┐    ┌─────────────┐
│    IMAP     │────│  Helpdesk Bridge │────│    Odoo     │
│   Server    │    │                  │    │  Helpdesk   │
└─────────────┘    │                  │    └─────────────┘
                   │                  │
┌─────────────┐    │                  │    ┌─────────────┐
│   Slack     │────│                  │────│    SMTP     │
│   Channel   │    │                  │    │   Server    │
└─────────────┘    └──────────────────┘    └─────────────┘
```

## Quick Start

### Using Docker (Recommended)

1. **Pull the image**:
```bash
docker pull ghcr.io/anaryk/odoo-helpdesk-bridge:latest
```

2. **Create configuration**:
```bash
cp config.yaml config-local.yaml
# Edit config-local.yaml with your settings
```

3. **Run the container**:
```bash
docker run -v ./config-local.yaml:/app/config/config.yaml \
           -v ./data:/app/data \
           ghcr.io/anaryk/odoo-helpdesk-bridge:latest
```

### Using Docker Compose

1. **Create docker-compose.yml**:
```yaml
version: '3.8'
services:
  helpdesk-bridge:
    image: ghcr.io/anaryk/odoo-helpdesk-bridge:latest
    volumes:
      - ./config.yaml:/app/config/config.yaml:ro
      - ./data:/app/data
    restart: unless-stopped
```

2. **Start the service**:
```bash
docker-compose up -d
```

### Building from Source

1. **Prerequisites**:
   - Go 1.24.1 or later
   - Git

2. **Clone and build**:
```bash
git clone https://github.com/anaryk/odoo-helpdesk-bridge.git
cd odoo-helpdesk-bridge
go build ./cmd/helpdesk-bridge
```

3. **Configure and run**:
```bash
cp config.yaml config-local.yaml
# Edit config-local.yaml
./helpdesk-bridge config-local.yaml
```

## Configuration

### Basic Configuration

```yaml
app:
  poll_seconds: 20          # Polling interval
  state_path: "./state.db"  # State database path
  ticket_prefix: "ML"       # Ticket ID prefix
  done_stage_ids: [21, 34]  # Odoo stage IDs for completed tickets
  sla:
    start_time_hours: 4     # Hours to start working on ticket
    resolution_time_hours: 24  # Hours to resolve ticket

odoo:
  url: "https://your-odoo.com"
  db: "your_db"
  username: "admin@company.com"
  password: "your_password"
  project_id: 123
  base_url: "https://your-odoo.com"
  timeout_seconds: 20

slack:
  webhook_url: "https://hooks.slack.com/services/XXX/YYY/ZZZ"
  bot_token: "xoxb-your-bot-token"    # For threading support
  channel_id: "C1234567890"           # Slack channel ID

imap:
  host: "imap.gmail.com"
  port: 993
  username: "support@company.com"
  password: "app-password"
  folder: "INBOX"
  search_to: "support@company.com"    # Filter emails
  custom_processed_flag: "X-HELPDESK"

smtp:
  host: "smtp.gmail.com"
  port: 587
  username: "support@company.com"
  password: "smtp-password"
  from_name: "Company Support"
  from_email: "support@company.com"
  timeout_seconds: 20
```

### Slack Setup

For full threading support, create a Slack Bot:

1. **Create Slack App**: Go to [api.slack.com](https://api.slack.com)
2. **Add Bot Token Scopes**:
   - `chat:write`
   - `chat:write.public`
3. **Install App**: Get the `xoxb-` bot token
4. **Get Channel ID**: Right-click channel → Copy link → Extract ID

### Email Setup

The service supports:
- **Gmail**: Use App Passwords
- **Office 365**: Use App Passwords or OAuth2
- **Other IMAP**: Standard authentication

## Usage

### Ticket Creation

When an email arrives:
1. **New Email** → Creates Odoo ticket → Slack notification with @channel
2. **Reply Email** (with ticket ID) → Adds comment to existing ticket

### Slack Interactions

- **New Ticket**: Posts to channel with @channel mention
- **Task Assignment**: Posts to thread when ticket is assigned
- **Task Completion**: Posts to thread when ticket is completed  
- **SLA Violations**: Posts to thread with @channel mention

### SLA Monitoring

The system tracks:
- **Start Time**: Time to begin working on ticket
- **Resolution Time**: Time to complete ticket

When violated:
- Adds label to Odoo ticket (`SLA_START_BREACH` or `SLA_RESOLUTION_BREACH`)
- Sends Slack notification to thread with @channel mention

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run linting
golangci-lint run
```

### Project Structure

```
├── cmd/helpdesk-bridge/     # Main application entry point
├── internal/                # Internal packages
│   ├── config/             # Configuration management
│   ├── imap/               # IMAP email processing
│   ├── odoo/               # Odoo API integration
│   ├── slack/              # Slack API integration
│   ├── mailer/             # SMTP email sending
│   ├── state/              # State management (BBolt)
│   ├── sla/                # SLA monitoring
│   └── templ/              # Template processing
├── templates/               # Email templates
├── .github/workflows/       # CI/CD pipelines
└── docs/                   # Documentation
```

### Architecture Decisions

- **State Management**: Uses BBolt for persistent state tracking
- **Threading**: Slack threads keep conversations organized
- **SLA Tracking**: Configurable time-based monitoring
- **Email Processing**: Supports both HTML and plain text
- **Error Handling**: Graceful degradation with logging

## Monitoring

### Health Checks

The Docker container includes a health check that monitors the main process.

### Logs

The application logs to stdout with structured information:
- Email processing status
- Odoo API interactions
- Slack notifications
- SLA violations

### Metrics

Consider integrating with:
- **Prometheus**: For metrics collection
- **Grafana**: For dashboards
- **Alertmanager**: For alerting

## Deployment

### Production Recommendations

1. **Resource Requirements**:
   - Memory: 128MB minimum, 256MB recommended
   - CPU: 0.1 cores minimum
   - Storage: 1GB for state database

2. **Security**:
   - Use app passwords instead of main passwords
   - Store secrets in environment variables or secret managers
   - Run as non-root user (default in Docker image)

3. **Backup**:
   - Backup `state.db` regularly
   - Monitor email processing queues

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: odoo-helpdesk-bridge
spec:
  replicas: 1
  selector:
    matchLabels:
      app: helpdesk-bridge
  template:
    metadata:
      labels:
        app: helpdesk-bridge
    spec:
      containers:
      - name: helpdesk-bridge
        image: ghcr.io/anaryk/odoo-helpdesk-bridge:latest
        volumeMounts:
        - name: config
          mountPath: /app/config
        - name: data
          mountPath: /app/data
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "200m"
      volumes:
      - name: config
        configMap:
          name: helpdesk-bridge-config
      - name: data
        persistentVolumeClaim:
          claimName: helpdesk-bridge-data
```

## Troubleshooting

### Common Issues

1. **IMAP Connection Failed**:
   - Check credentials and server settings
   - Verify firewall allows IMAP connections
   - Enable "Less secure apps" for Gmail

2. **Odoo API Errors**:
   - Verify Odoo URL and database name
   - Check user permissions for project access
   - Ensure API access is enabled

3. **Slack Notifications Not Working**:
   - Verify webhook URL or bot token
   - Check channel permissions
   - Ensure bot is invited to channel

4. **SLA Not Triggering**:
   - Check SLA configuration times
   - Verify Odoo stage names/IDs
   - Review state database for task entries

### Debug Mode

Add verbose logging by modifying the log level in your deployment.

## Contributing

1. **Fork** the repository
2. **Create** a feature branch: `git checkout -b feature/amazing-feature`
3. **Commit** your changes: `git commit -m 'Add amazing feature'`
4. **Push** to branch: `git push origin feature/amazing-feature`
5. **Open** a Pull Request

### Development Setup

```bash
# Clone your fork
git clone https://github.com/your-username/odoo-helpdesk-bridge.git
cd odoo-helpdesk-bridge

# Install dependencies
go mod download

# Run tests
go test ./...

# Build
go build ./cmd/helpdesk-bridge
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history and changes.

## Support

- 🐛 **Bug Reports**: [GitHub Issues](https://github.com/anaryk/odoo-helpdesk-bridge/issues)
- 💡 **Feature Requests**: [GitHub Issues](https://github.com/anaryk/odoo-helpdesk-bridge/issues)
- 📖 **Documentation**: [GitHub Wiki](https://github.com/anaryk/odoo-helpdesk-bridge/wiki)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/anaryk/odoo-helpdesk-bridge/discussions)