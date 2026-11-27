# Stage 1: Build the Go YouTube search microservice
FROM golang:1.26-alpine AS builder
WORKDIR /src/youtube
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/youtube-service .

# Stage 2: Minimal runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/youtube-service /app/youtube-service

EXPOSE 8081
CMD ["/app/youtube-service"]
