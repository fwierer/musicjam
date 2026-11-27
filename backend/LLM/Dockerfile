# Stage 1: Build the Go LLM microservice
FROM golang:1.26-alpine AS builder
WORKDIR /src/llm
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/llm-service .

# Stage 2: Minimal runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/llm-service /app/llm-service

EXPOSE 8082
CMD ["/app/llm-service"]
