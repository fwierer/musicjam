#!/bin/sh
set -eu

ollama serve &
OLLAMA_PID=$!

# Wait until the bundled Ollama server is accepting connections
i=0
while [ "$i" -lt 30 ]; do
  if ollama list >/dev/null 2>&1; then
    break
  fi
  i=$((i + 1))
  sleep 1
done

MODEL="${OLLAMA_MODEL:-qwen3:4b}"
if [ "${OLLAMA_AUTO_PULL:-true}" = "true" ] && ! ollama list | awk '{print $1}' | grep -Fxq "$MODEL"; then
  echo "Pulling Ollama model $MODEL in background..."
  ollama pull "$MODEL" &
fi

trap 'kill "$OLLAMA_PID" 2>/dev/null || true' TERM INT
exec /app/llm-service
