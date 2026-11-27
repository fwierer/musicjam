# Stage 1: Build the Go Player fallback microservice
FROM golang:1.26-alpine AS builder
WORKDIR /src/player
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/player-service .

# Stage 2: Minimal runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/player-service /app/player-service

EXPOSE 8083
CMD ["/app/player-service"]
