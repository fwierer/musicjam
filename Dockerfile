# Stage 1: Build the main Go application
FROM golang:1.26-alpine AS builder
WORKDIR /src/backend
COPY backend/go.mod ./
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/musicjam .

# Stage 2: Minimal runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/musicjam /app/musicjam
COPY frontend/ /app/frontend/
RUN mkdir -p /app/database

ENV FRONTEND_DIR=/app/frontend \
    DATA_DIR=/app/database

EXPOSE 8000
CMD ["/app/musicjam"]
