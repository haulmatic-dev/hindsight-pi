#!/bin/bash
# Hindsight Environment Configuration
# Source this file before starting Hindsight: source /Users/buddhi/source-code/personal/hydra/scripts/hindsight_env.sh

# === LLM Configuration (for Retain/Reflect operations) ===
# Using Kimi via Anthropic-compatible API (coding endpoint)
export HINDSIGHT_API_LLM_PROVIDER="anthropic"
export HINDSIGHT_API_LLM_API_KEY="${KIMI_API_KEY}"
export HINDSIGHT_API_LLM_BASE_URL="https://api.kimi.com/coding"
export HINDSIGHT_API_LLM_MODEL="kimi-k2-thinking"

# Per-operation LLM configuration (inherits from above by default)
export HINDSIGHT_API_RETAIN_LLM_PROVIDER="anthropic"
export HINDSIGHT_API_RETAIN_LLM_API_KEY="${KIMI_API_KEY}"
export HINDSIGHT_API_RETAIN_LLM_BASE_URL="https://api.kimi.com/coding"
export HINDSIGHT_API_RETAIN_LLM_MODEL="kimi-k2-thinking"

export HINDSIGHT_API_REFLECT_LLM_PROVIDER="anthropic"
export HINDSIGHT_API_REFLECT_LLM_API_KEY="${KIMI_API_KEY}"
export HINDSIGHT_API_REFLECT_LLM_BASE_URL="https://api.kimi.com/coding"
export HINDSIGHT_API_REFLECT_LLM_MODEL="kimi-k2-thinking"

# === Embeddings Configuration (for vector search) ===
# Using OpenAI to save local memory (saves 3-5GB)
export HINDSIGHT_API_EMBEDDINGS_PROVIDER="openai"
export HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY="${OPENAI_API_KEY}"
export HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL="text-embedding-3-small"

# === Reranker Configuration ===
# Using FlashRank (CPU-based, ~34MB memory, FREE)
export HINDSIGHT_API_RERANKER_PROVIDER="flashrank"
export HINDSIGHT_API_RERANKER_FLASHRANK_MODEL="ms-marco-MiniLM-L-12-v2"

# === Database Configuration ===
# Reduce connection pool to save memory
export HINDSIGHT_API_DB_POOL_MAX_SIZE="20"

# === Logging ===
export HINDSIGHT_API_LOG_LEVEL="info"

echo "✅ Hindsight environment configured:"
echo "   LLM: Kimi (kimi-k2-thinking) via Anthropic API"
echo "   Embeddings: OpenAI (text-embedding-3-small)"
echo "   Reranker: FlashRank (CPU-based)"
echo ""
echo "Key checks:"
if [ -n "$KIMI_API_KEY" ]; then
    echo "   KIMI_API_KEY: ✅ Set"
else
    echo "   KIMI_API_KEY: ❌ Missing"
fi
if [ -n "$OPENAI_API_KEY" ]; then
    echo "   OPENAI_API_KEY: ✅ Set"  
else
    echo "   OPENAI_API_KEY: ❌ Missing"
fi
