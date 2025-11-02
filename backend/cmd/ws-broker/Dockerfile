# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy entire backend source
COPY . .

# Build ws-broker binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /ws-broker ./cmd/ws-broker

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Copy binary from builder
COPY --from=builder /ws-broker /app/ws-broker

# Expose port
EXPOSE 8080

# Run the ws-broker
CMD ["/app/ws-broker"]

