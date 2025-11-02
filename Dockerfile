# Build stage
FROM golang:1.24.9-alpine AS builder

# Install necessary packages
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY templates/ ./templates/

# Build the application
RUN go build -o helpdesk-bridge ./cmd/helpdesk-bridge

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

# Create app user
RUN addgroup -g 1001 appgroup && \
    adduser -u 1001 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/helpdesk-bridge .

# Copy templates directory
COPY templates/ ./templates/

# Change ownership to app user
RUN chown -R appuser:appgroup /app

# Switch to app user
USER appuser

# Create data directory for state
RUN mkdir -p /app/data

# Expose port (if needed for health checks)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD pgrep helpdesk-bridge || exit 1

# Command to run
ENTRYPOINT ["./helpdesk-bridge"]
CMD ["./config/config.yaml"]