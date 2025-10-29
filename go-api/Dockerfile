# Build stage
FROM golang:1.21 AS builder
WORKDIR /app

COPY go.mod ./
COPY proto/ proto/
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./main.go

# Runtime stage
FROM alpine:3.19
WORKDIR /app

RUN apk add --no-cache ca-certificates curl

COPY --from=builder /app/server ./server

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD curl -f http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app/server"]
