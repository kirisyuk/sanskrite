FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /hatlone-ws ./cmd/hatlone-ws

FROM alpine:latest

WORKDIR /app
RUN apk --no-cache add ca-certificates
COPY --from=builder /hatlone-ws /app/hatlone-ws

EXPOSE 8080
CMD ["/app/hatlone-ws"]


