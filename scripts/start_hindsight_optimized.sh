#!/bin/bash
# Start Hindsight with memory-optimized configuration
# Uses OpenAI for embeddings and FlashRank for reranking
# Saves 4-7 GB of memory compared to local models

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🚀 Starting Hindsight with Memory Optimization${NC}"
echo ""

# Check for OpenAI API key
if [ -z "$OPENAI_API_KEY" ] && [ -z "$HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY" ]; then
    echo -e "${RED}❌ Error: OpenAI API key not found${NC}"
    echo ""
    echo "Please set your OpenAI API key:"
    echo "  export OPENAI_API_KEY=sk-your-key"
    echo ""
    echo "Or use the standard Hindsight environment variable:"
    echo "  export HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY=sk-your-key"
    exit 1
fi

# Use OPENAI_API_KEY if HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY is not set
if [ -z "$HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY" ] && [ -n "$OPENAI_API_KEY" ]; then
    export HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY="$OPENAI_API_KEY"
    echo -e "${YELLOW}⚠️  Using OPENAI_API_KEY for embeddings${NC}"
fi

# Set optimized configuration
echo -e "${GREEN}📋 Configuration:${NC}"
echo "  • Embeddings: OpenAI (text-embedding-3-small)"
echo "  • Reranking: FlashRank (CPU-based, ~34MB)"
echo "  • DB Pool: 20 connections (reduced from 100)"
echo ""

export HINDSIGHT_API_EMBEDDINGS_PROVIDER=openai
export HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL=text-embedding-3-small

export HINDSIGHT_API_RERANKER_PROVIDER=flashrank
export HINDSIGHT_API_RERANKER_FLASHRANK_MODEL=ms-marco-MiniLM-L-12-v2

export HINDSIGHT_API_DB_POOL_MAX_SIZE=20

# Optional: Disable history tracking to save storage
# export HINDSIGHT_API_ENABLE_MENTAL_MODEL_HISTORY=false
# export HINDSIGHT_API_ENABLE_OBSERVATION_HISTORY=false

# Set LLM provider for retain/reflect operations
export HINDSIGHT_API_LLM_PROVIDER=openai
export HINDSIGHT_API_LLM_API_KEY="$HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY"
export HINDSIGHT_API_LLM_MODEL=gpt-4o-mini

echo -e "${GREEN}🔧 Starting Hindsight...${NC}"
echo ""

# Check if hindsight is available
if ! command -v hindsight-api &> /dev/null; then
    echo -e "${RED}❌ Error: hindsight-api not found in PATH${NC}"
    echo ""
    echo "Please install Hindsight first:"
    echo "  pip install hindsight-all"
    exit 1
fi

# Start Hindsight
python3 -c "
from hindsight import HindsightEmbedded
import sys

try:
    client = HindsightEmbedded(profile='hydra')
    print(f'✅ Hindsight started successfully at: {client.url}')
    print('')
    print('💾 Memory-optimized configuration active:')
    print('  • Expected memory usage: 3-6 GB (down from 10 GB)')
    print('  • Using cloud embeddings (OpenAI)')
    print('  • Using CPU-based reranking (FlashRank)')
    print('')
    print('Press Ctrl+C to stop...')
    
    # Keep running
    import time
    while True:
        time.sleep(1)
        
except KeyboardInterrupt:
    print('\n\n👋 Shutting down Hindsight...')
    client.close()
    print('✅ Hindsight stopped')
    sys.exit(0)
except Exception as e:
    print(f'\n❌ Error: {e}', file=sys.stderr)
    sys.exit(1)
"
