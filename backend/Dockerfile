# Build stage
FROM golang:1.25.1-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

# Runtime stage
FROM alpine:latest

WORKDIR /app

# Install ca-certificates for HTTPS requests and Pandoc for markdown to docx conversion
RUN apk --no-cache add ca-certificates pandoc

# Copy binary from builder
COPY --from=builder /server /app/server

# Expose port
EXPOSE 8080

# Run the application
CMD ["/app/server"]
