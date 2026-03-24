#!/bin/bash
# Monitor Hindsight memory usage and restart if it exceeds threshold
# Usage: ./monitor_hindsight.sh [max_memory_mb] [check_interval_seconds]

MAX_MEMORY_MB=${1:-8192}  # Default 8GB
CHECK_INTERVAL=${2:-60}   # Default 60 seconds
PORT=9418

echo "🔍 Starting Hindsight memory monitor"
echo "   Max memory: ${MAX_MEMORY_MB} MB"
echo "   Check interval: ${CHECK_INTERVAL} seconds"
echo ""

while true; do
    # Find Hindsight PID
    HINDSIGHT_PID=$(pgrep -f "hindsight-api.*--port $PORT" | head -1)
    
    if [ -z "$HINDSIGHT_PID" ]; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') ⚠️  Hindsight not running"
        sleep $CHECK_INTERVAL
        continue
    fi
    
    # Get memory usage in MB
    MEMORY_KB=$(ps -o rss= -p $HINDSIGHT_PID 2>/dev/null | tr -d ' ')
    if [ -z "$MEMORY_KB" ]; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') ⚠️  Could not get memory for PID $HINDSIGHT_PID"
        sleep $CHECK_INTERVAL
        continue
    fi
    
    MEMORY_MB=$((MEMORY_KB / 1024))
    
    # Show status
    echo "$(date '+%Y-%m-%d %H:%M:%S') 📊 Hindsight memory: ${MEMORY_MB} MB (PID: $HINDSIGHT_PID)"
    
    # Check if exceeds threshold
    if [ "$MEMORY_MB" -gt "$MAX_MEMORY_MB" ]; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') 🚨 Memory limit exceeded! (${MEMORY_MB} MB > ${MAX_MEMORY_MB} MB)"
        echo "$(date '+%Y-%m-%d %H:%M:%S') 🔄 Restarting Hindsight..."
        
        # Restart Hindsight
        /Users/buddhi/source-code/personal/hydra/scripts/restart_hindsight.sh $PORT
        
        echo "$(date '+%Y-%m-%d %H:%M:%S') ✅ Restart complete"
    fi
    
    sleep $CHECK_INTERVAL
done
