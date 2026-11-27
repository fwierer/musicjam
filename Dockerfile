# Stage 1: Build the Jam Session Core Go API
FROM golang:1.26-alpine AS builder
WORKDIR /src/backend
COPY backend/go.mod ./
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/musicjam .

# Stage 2: Minimal API-only runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/musicjam /app/musicjam

EXPOSE 8000
CMD ["/app/musicjam"]
