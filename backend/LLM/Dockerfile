# Stage 1: Build the Go LLM moderation microservice
FROM golang:1.26-alpine AS builder
WORKDIR /src/llm
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/llm-service .

# Stage 2: Bundle Go moderation API with Ollama runtime in one container
FROM ollama/ollama:latest
WORKDIR /app
COPY --from=builder /out/llm-service /app/llm-service
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

ENV PORT=8082 \
    OLLAMA_URL=http://127.0.0.1:11434

EXPOSE 8082
ENTRYPOINT ["/app/entrypoint.sh"]
CMD []
