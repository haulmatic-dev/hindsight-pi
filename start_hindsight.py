#!/usr/bin/env python3
"""
Hindsight Startup Script with Memory Optimization
Uses:
- Kimi for LLM via Anthropic API (retain/reflect operations)
- OpenAI for embeddings (saves 3-5GB memory)
- FlashRank for reranking (CPU-based, ~34MB)
"""

import os
import sys
import time

# === LLM Configuration (Retain/Reflect) ===
# Using Kimi via Anthropic-compatible API (coding endpoint)
os.environ.setdefault("HINDSIGHT_API_LLM_PROVIDER", "anthropic")
os.environ.setdefault("HINDSIGHT_API_LLM_API_KEY", os.environ.get("KIMI_API_KEY", ""))
os.environ.setdefault("HINDSIGHT_API_LLM_BASE_URL", "https://api.kimi.com/coding")
os.environ.setdefault("HINDSIGHT_API_LLM_MODEL", "kimi-k2-thinking")

# Per-operation LLM configuration
os.environ.setdefault("HINDSIGHT_API_RETAIN_LLM_PROVIDER", "anthropic")
os.environ.setdefault(
    "HINDSIGHT_API_RETAIN_LLM_API_KEY", os.environ.get("KIMI_API_KEY", "")
)
os.environ.setdefault(
    "HINDSIGHT_API_RETAIN_LLM_BASE_URL", "https://api.kimi.com/coding"
)
os.environ.setdefault("HINDSIGHT_API_RETAIN_LLM_MODEL", "kimi-k2-thinking")

os.environ.setdefault("HINDSIGHT_API_REFLECT_LLM_PROVIDER", "anthropic")
os.environ.setdefault(
    "HINDSIGHT_API_REFLECT_LLM_API_KEY", os.environ.get("KIMI_API_KEY", "")
)
os.environ.setdefault(
    "HINDSIGHT_API_REFLECT_LLM_BASE_URL", "https://api.kimi.com/coding"
)
os.environ.setdefault("HINDSIGHT_API_REFLECT_LLM_MODEL", "kimi-k2-thinking")

# === Embeddings Configuration (Vector Search) ===
# Using OpenAI to save local memory (saves 3-5GB)
os.environ.setdefault("HINDSIGHT_API_EMBEDDINGS_PROVIDER", "openai")
os.environ.setdefault(
    "HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY", os.environ.get("OPENAI_API_KEY", "")
)
os.environ.setdefault("HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL", "text-embedding-3-small")

# === Reranker Configuration ===
# Using FlashRank (CPU-based, ~34MB memory, FREE)
os.environ.setdefault("HINDSIGHT_API_RERANKER_PROVIDER", "flashrank")
os.environ.setdefault(
    "HINDSIGHT_API_RERANKER_FLASHRANK_MODEL", "ms-marco-MiniLM-L-12-v2"
)

# === Database ===
os.environ.setdefault("HINDSIGHT_API_DB_POOL_MAX_SIZE", "20")
os.environ.setdefault("HINDSIGHT_API_LOG_LEVEL", "info")

# Validate required keys
kimi_key = os.environ.get("HINDSIGHT_API_LLM_API_KEY")
openai_key = os.environ.get("HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY")

print("=" * 60)
print("Hindsight Memory-Optimized Startup")
print("=" * 60)

if not kimi_key:
    print("❌ ERROR: KIMI_API_KEY not set!")
    print("   Please set KIMI_API_KEY environment variable")
    sys.exit(1)
else:
    print(f"✅ Kimi API Key: {kimi_key[:20]}...")

if not openai_key:
    print("⚠️  WARNING: OPENAI_API_KEY not set!")
    print("   Embeddings will use local models (uses more memory)")
    print("   Set OPENAI_API_KEY for cloud embeddings to save 3-5GB")
else:
    print(f"✅ OpenAI API Key: {openai_key[:20]}...")

print(f"\nConfiguration:")
print(f"   LLM Provider: {os.environ['HINDSIGHT_API_LLM_PROVIDER']}")
print(f"   LLM Model: {os.environ['HINDSIGHT_API_LLM_MODEL']}")
print(f"   LLM Base URL: {os.environ['HINDSIGHT_API_LLM_BASE_URL']}")
print(f"   Embeddings: {os.environ['HINDSIGHT_API_EMBEDDINGS_PROVIDER']}")
print(f"   Reranker: {os.environ['HINDSIGHT_API_RERANKER_PROVIDER']}")
print(f"   Port: 9418")
print()

# Start Hindsight using hindsight_embed
try:
    from hindsight_embed import get_embed_manager

    print("Creating embed manager...")
    manager = get_embed_manager()

    print(f"✅ Hindsight daemon started at port 9418")
    print(f"   URL: http://127.0.0.1:9418")
    print()
    print("Press Ctrl+C to stop")

    # Keep running
    while True:
        try:
            time.sleep(1)
        except KeyboardInterrupt:
            print("\n🛑 Shutting down...")
            break

except ImportError as e:
    print(f"❌ Failed to import hindsight_embed: {e}")
    print("   Make sure hindsight-embed is installed: uv pip install hindsight-embed")
    sys.exit(1)
except Exception as e:
    print(f"❌ Error starting Hindsight: {e}")
    import traceback

    traceback.print_exc()
    sys.exit(1)
