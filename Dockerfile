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

# Copy source code - copy everything except what's in .dockerignore
COPY . .

# Debug: List what was copied
RUN echo "=== Root directory contents ===" && ls -la
RUN echo "=== Checking cmd directory ===" && ls -la cmd/ || echo "cmd directory not found"
RUN echo "=== Checking cmd/helpdesk-bridge directory ===" && ls -la cmd/helpdesk-bridge/ || echo "cmd/helpdesk-bridge directory not found"

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o helpdesk-bridge ./cmd/helpdesk-bridge

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