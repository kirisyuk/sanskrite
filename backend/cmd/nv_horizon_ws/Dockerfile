FROM golang:1.25.1-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /nv_horizon_ws ./cmd/nv_horizon_ws

FROM alpine:latest

WORKDIR /app
RUN apk --no-cache add ca-certificates
COPY --from=builder /nv_horizon_ws /app/nv_horizon_ws

EXPOSE 8080
CMD ["/app/nv_horizon_ws"]

