#!/bin/sh
# Start Hindsight with memory-optimized configuration
# Uses OpenAI for embeddings and FlashRank for reranking
# Saves 4-7 GB of memory compared to local models
# 
# This script respects environment variables from ~/.zshrc, ~/.bashrc, or any shell
# Required env vars: OPENAI_API_KEY or HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY
#
# Usage: ./start_hindsight_optimized.sh

# POSIX-compliant color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo ""
echo -e "${GREEN}🚀 Starting Hindsight with Memory Optimization${NC}"
echo ""

# Check for OpenAI API key (works in any shell)
if [ -z "$OPENAI_API_KEY" ] && [ -z "$HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY" ]; then
    echo -e "${RED}❌ Error: OpenAI API key not found${NC}"
    echo ""
    echo "Please add to your ~/.zshrc or ~/.bashrc:"
    echo -e "${BLUE}  export OPENAI_API_KEY=sk-your-key${NC}"
    echo ""
    echo "Then reload your shell:"
    echo -e "${BLUE}  source ~/.zshrc  # or source ~/.bashrc${NC}"
    echo ""
    echo "Alternatively, set it temporarily:"
    echo -e "${BLUE}  export OPENAI_API_KEY=sk-your-key${NC}"
    exit 1
fi

# Use OPENAI_API_KEY if HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY is not set
if [ -z "$HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY" ] && [ -n "$OPENAI_API_KEY" ]; then
    export HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY="$OPENAI_API_KEY"
    echo -e "${YELLOW}⚠️  Using OPENAI_API_KEY for embeddings${NC}"
    echo ""
fi

# Set optimized configuration (only if not already set)
# This allows ~/.zshrc values to override defaults

# Embeddings provider
if [ -z "$HINDSIGHT_API_EMBEDDINGS_PROVIDER" ]; then
    export HINDSIGHT_API_EMBEDDINGS_PROVIDER=openai
fi

# Embeddings model
if [ -z "$HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL" ]; then
    export HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL=text-embedding-3-small
fi

# Reranker provider
if [ -z "$HINDSIGHT_API_RERANKER_PROVIDER" ]; then
    export HINDSIGHT_API_RERANKER_PROVIDER=flashrank
fi

# Reranker model
if [ -z "$HINDSIGHT_API_RERANKER_FLASHRANK_MODEL" ]; then
    export HINDSIGHT_API_RERANKER_FLASHRANK_MODEL=ms-marco-MiniLM-L-12-v2
fi

# DB Pool size
if [ -z "$HINDSIGHT_API_DB_POOL_MAX_SIZE" ]; then
    export HINDSIGHT_API_DB_POOL_MAX_SIZE=20
fi

# LLM provider for retain/reflect
if [ -z "$HINDSIGHT_API_LLM_PROVIDER" ]; then
    export HINDSIGHT_API_LLM_PROVIDER=openai
fi

if [ -z "$HINDSIGHT_API_LLM_API_KEY" ]; then
    export HINDSIGHT_API_LLM_API_KEY="$HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY"
fi

if [ -z "$HINDSIGHT_API_LLM_MODEL" ]; then
    export HINDSIGHT_API_LLM_MODEL=gpt-4o-mini
fi

# Display configuration
echo -e "${GREEN}📋 Active Configuration:${NC}"
echo "  • Embeddings Provider: $HINDSIGHT_API_EMBEDDINGS_PROVIDER"
echo "  • Embeddings Model: $HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL"
echo "  • Reranker Provider: $HINDSIGHT_API_RERANKER_PROVIDER"
echo "  • Reranker Model: $HINDSIGHT_API_RERANKER_FLASHRANK_MODEL"
echo "  • DB Pool Size: $HINDSIGHT_API_DB_POOL_MAX_SIZE"
echo "  • LLM Model: $HINDSIGHT_API_LLM_MODEL"
echo ""

# Check if Hindsight is available
if ! command -v python3 >/dev/null 2>&1; then
    echo -e "${RED}❌ Error: python3 not found${NC}"
    echo "Please install Python 3"
    exit 1
fi

echo -e "${GREEN}🔧 Starting Hindsight...${NC}"
echo ""

# Start Hindsight with embedded Python script
python3 << 'PYTHON_SCRIPT'
from hindsight import HindsightEmbedded
import sys
import os

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
    try:
        client.close()
    except:
        pass
    print('✅ Hindsight stopped')
    sys.exit(0)
except Exception as e:
    print(f'\n❌ Error: {e}', file=sys.stderr)
    sys.exit(1)
PYTHON_SCRIPT
