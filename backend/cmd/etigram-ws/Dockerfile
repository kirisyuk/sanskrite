FROM golang:1.25.1-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /etigram-ws ./cmd/etigram-ws

FROM alpine:latest

WORKDIR /app
RUN apk --no-cache add ca-certificates
COPY --from=builder /etigram-ws /app/etigram-ws

EXPOSE 8080
CMD ["/app/etigram-ws"]


