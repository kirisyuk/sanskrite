FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files from backend root
COPY go.mod go.sum ./
RUN go mod download

# Copy entire backend source (needed for internal packages)
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /verified-proxy ./cmd/verified-proxy

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /verified-proxy .

EXPOSE 8080

CMD ["./verified-proxy"]

