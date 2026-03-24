#!/bin/bash
# Script to restart Hindsight server to free GPU memory
# Usage: ./restart_hindsight.sh [port]

PORT=${1:-9418}

echo "🔍 Checking for existing Hindsight processes..."

# Find and kill existing Hindsight processes
HINDSIGHT_PIDS=$(pgrep -f "hindsight-api.*--port $PORT" || echo "")
PYTHON_PIDS=$(pgrep -f "start_hindsight.py" || echo "")

if [ -n "$HINDSIGHT_PIDS" ]; then
    echo "🛑 Stopping Hindsight API processes: $HINDSIGHT_PIDS"
    echo "$HINDSIGHT_PIDS" | xargs -I {} kill -15 {} 2>/dev/null
    sleep 2
    # Force kill if still running
    echo "$HINDSIGHT_PIDS" | xargs -I {} kill -9 {} 2>/dev/null
fi

if [ -n "$PYTHON_PIDS" ]; then
    echo "🛑 Stopping Python Hindsight processes: $PYTHON_PIDS"
    echo "$PYTHON_PIDS" | xargs -I {} kill -15 {} 2>/dev/null
    sleep 2
    # Force kill if still running
    echo "$PYTHON_PIDS" | xargs -I {} kill -9 {} 2>/dev/null
fi

# Wait for processes to fully terminate
sleep 3

# Check if port is now free
if lsof -i :$PORT >/dev/null 2>&1; then
    echo "⚠️  Warning: Port $PORT is still in use. Trying to free it..."
    lsof -ti :$PORT | xargs -I {} kill -9 {} 2>/dev/null
    sleep 2
fi

echo "🚀 Starting Hindsight server..."
python3 /Users/buddhi/source-code/personal/hydra/start_hindsight.py &

# Wait for server to start
sleep 5

# Check if server is running
if curl -s http://127.0.0.1:$PORT/health >/dev/null 2>&1; then
    echo "✅ Hindsight server restarted successfully on port $PORT"
    
    # Show memory usage after restart
    HINDSIGHT_PID=$(pgrep -f "hindsight-api.*--port $PORT" | head -1)
    if [ -n "$HINDSIGHT_PID" ]; then
        MEMORY_MB=$(ps -o rss= -p $HINDSIGHT_PID | awk '{print int($1/1024)}')
        echo "📊 Current memory usage: ${MEMORY_MB} MB"
    fi
else
    echo "❌ Failed to restart Hindsight server"
    exit 1
fi
