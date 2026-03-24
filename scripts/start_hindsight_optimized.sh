#!/bin/bash
# Start Hindsight with memory-optimized configuration

set -e

echo "🚀 Starting Hindsight with memory optimization..."
echo ""

# Source .zshrc to get API keys (in case we're in a non-interactive shell)
if [ -f "$HOME/.zshrc" ]; then
    source "$HOME/.zshrc" 2>/dev/null || true
fi

# Source the environment configuration
ENV_FILE="${0%/*}/hindsight_env.sh"
if [ -f "$ENV_FILE" ]; then
    echo "Loading configuration from: $ENV_FILE"
    source "$ENV_FILE"
else
    echo "⚠️  Warning: Environment file not found at $ENV_FILE"
    echo "   Using current environment variables"
fi

# Check required keys
echo ""
echo "Checking API keys..."

if [ -z "$KIMI_API_KEY" ]; then
    echo "❌ ERROR: KIMI_API_KEY is not set!"
    echo "   Please set it: export KIMI_API_KEY='your-key'"
    exit 1
else
    echo "✅ KIMI_API_KEY is set"
fi

if [ -z "$OPENAI_API_KEY" ]; then
    echo "⚠️  WARNING: OPENAI_API_KEY is not set!"
    echo "   Embeddings will use local models (uses more memory)"
    echo "   Set it for cloud embeddings: export OPENAI_API_KEY='your-key'"
    echo ""
    read -p "Continue without OpenAI? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
else
    echo "✅ OPENAI_API_KEY is set"
fi

echo ""
echo "Configuration:"
echo "   LLM: Kimi (kimi-k2-5)"
echo "   Embeddings: ${HINDSIGHT_API_EMBEDDINGS_PROVIDER:-openai}"
echo "   Reranker: ${HINDSIGHT_API_RERANKER_PROVIDER:-flashrank}"
echo "   Port: 9418"
echo ""

# Kill any existing Hindsight processes
echo "Cleaning up existing processes..."
pkill -9 -f hindsight-api 2>/dev/null || true
pkill -9 -f start_hindsight.py 2>/dev/null || true
sleep 2

# Check if port is free
if lsof -i :9418 >/dev/null 2>&1; then
    echo "⚠️  Port 9418 is still in use. Trying to free it..."
    lsof -ti :9418 | xargs -r kill -9 2>/dev/null || true
    sleep 2
fi

# Start Hindsight
echo "Starting Hindsight server..."
cd /Users/buddhi/source-code/personal/hydra
python3 start_hindsight.py &
PID=$!

echo "Started with PID: $PID"
echo ""
echo "Waiting for server to be ready..."

# Wait for health check
for i in {1..30}; do
    if curl -s http://127.0.0.1:9418/health >/dev/null 2>&1; then
        echo ""
        echo "✅ Hindsight is running!"
        echo "   URL: http://127.0.0.1:9418"
        echo "   PID: $PID"
        
        # Show memory usage
        sleep 2
        MEMORY_KB=$(ps -o rss= -p $PID 2>/dev/null | tr -d ' ')
        if [ -n "$MEMORY_KB" ]; then
            MEMORY_MB=$((MEMORY_KB / 1024))
            echo "   Memory: ${MEMORY_MB} MB"
        fi
        
        # Save PID for later
        echo $PID > /tmp/hindsight.pid
        
        echo ""
        echo "To stop: kill $PID"
        echo "To monitor: ./scripts/monitor_hindsight.sh"
        exit 0
    fi
    echo -n "."
    sleep 1
done

echo ""
echo "❌ Failed to start Hindsight within 30 seconds"
echo "Check logs: tail -50 /tmp/hindsight.log"
kill $PID 2>/dev/null || true
exit 1
