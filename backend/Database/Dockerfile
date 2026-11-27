# Stage 1: Build the Go JSON Database microservice
FROM golang:1.26-alpine AS builder
WORKDIR /src/database
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/database-service .

# Stage 2: Minimal runtime image
FROM alpine:3.21
WORKDIR /app
COPY --from=builder /out/database-service /app/database-service
RUN mkdir -p /app/database

ENV PORT=8084 \
    DATA_DIR=/app/database

EXPOSE 8084
CMD ["/app/database-service"]
