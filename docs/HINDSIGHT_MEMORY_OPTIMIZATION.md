> **DEPRECATED (2026-11-08):** this guide predates the unified memory system. Superseded by the memory-system architecture and config-as-code. Current truth: `docs/operations/memory-system.md` (architecture, pipelines, quality harness) and the infra repo `services/hindsight/` (config + runbooks).

# Hindsight Memory Optimization Guide

## Overview

Hindsight uses significant memory (7-10 GB) due to loaded ML/embedding models. This guide explains why this happens and how to optimize memory usage through cloud-based providers.

## Table of Contents

1. [Why Does Hindsight Use So Much Memory?](#why-does-hindsight-use-so-much-memory)
2. [Configuration Options](#configuration-options)
3. [Recommended Setup](#recommended-setup)
4. [Alternative Providers](#alternative-providers)
5. [Cost Analysis](#cost-analysis)
6. [Troubleshooting](#troubleshooting)

---

## Why Does Hindsight Use So Much Memory?

### Root Cause

Hindsight uses 7-10 GB of memory because of:

1. **Local Embedding Models** (~3-5 GB)
   - Default: `BAAI/bge-small-en-v1.5` (384 dimensions)
   - Loaded into GPU/CPU memory for fast embedding generation
   - Model weights stay resident for performance

2. **Local Reranker Models** (~1-2 GB)
   - Default: `cross-encoder/ms-marco-MiniLM-L-6-v2` (~34MB) or larger models
   - Used for ranking search results
   - Also stays loaded in memory

3. **Embedding Cache** (~2-3 GB)
   - Accumulates embeddings for faster repeated queries
   - Grows over time as more memories are stored

4. **Python Runtime & Dependencies** (~1 GB)
   - PostgreSQL embedded (pg0)
   - Python ML libraries (PyTorch, Transformers, etc.)

### Why Memory Stays High After Completion

Even after all beads are processed, memory remains high because:
- **ML models stay loaded** for fast subsequent queries
- **Embedding cache** accumulates to improve performance
- **GPU memory is not released** until process termination

This is by design for production ML services - unloading/reloading models would be too slow.

---

## Configuration Options

### Environment Variables Reference

All Hindsight configuration is done via environment variables with the `HINDSIGHT_API_*` prefix.

#### Embedding Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `HINDSIGHT_API_EMBEDDINGS_PROVIDER` | Provider: `local`, `openai`, `cohere`, `tei`, `litellm`, `litellm-sdk` | `local` |
| `HINDSIGHT_API_EMBEDDINGS_LOCAL_MODEL` | Model for local provider | `BAAI/bge-small-en-v1.5` |
| `HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY` | OpenAI API key | - |
| `HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL` | OpenAI embedding model | `text-embedding-3-small` |
| `HINDSIGHT_API_EMBEDDINGS_COHERE_API_KEY` | Cohere API key | - |
| `HINDSIGHT_API_EMBEDDINGS_COHERE_MODEL` | Cohere embedding model | `embed-english-v3.0` |

#### Reranker Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `HINDSIGHT_API_RERANKER_PROVIDER` | Provider: `local`, `flashrank`, `cohere`, `zeroentropy`, `tei`, `litellm`, `litellm-sdk` | `local` |
| `HINDSIGHT_API_RERANKER_LOCAL_MODEL` | Model for local provider | `cross-encoder/ms-marco-MiniLM-L-6-v2` |
| `HINDSIGHT_API_RERANKER_FLASHRANK_MODEL` | FlashRank model | `ms-marco-MiniLM-L-12-v2` |
| `HINDSIGHT_API_RERANKER_COHERE_API_KEY` | Cohere API key | - |
| `HINDSIGHT_API_RERANKER_COHERE_MODEL` | Cohere rerank model | `rerank-english-v3.0` |

#### Database Connection Pool

| Variable | Description | Default |
|----------|-------------|---------|
| `HINDSIGHT_API_DB_POOL_MIN_SIZE` | Minimum connections in pool | `5` |
| `HINDSIGHT_API_DB_POOL_MAX_SIZE` | Maximum connections in pool | `100` |
| `HINDSIGHT_API_DB_COMMAND_TIMEOUT` | PostgreSQL command timeout (seconds) | `60` |
| `HINDSIGHT_API_DB_ACQUIRE_TIMEOUT` | Connection acquisition timeout (seconds) | `30` |

#### Memory-Related Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `HINDSIGHT_API_ENABLE_MENTAL_MODEL_HISTORY` | Track mental model changes | `true` |
| `HINDSIGHT_API_ENABLE_OBSERVATION_HISTORY` | Track observation changes | `true` |
| `HINDSIGHT_API_CONSOLIDATION_BATCH_SIZE` | Memories per batch | `50` |

---

## Recommended Setup

### Option A: OpenAI + FlashRank (RECOMMENDED)

This setup uses OpenAI for embeddings (cloud-based, saves ~3-5GB) and FlashRank for reranking (tiny CPU models, saves ~1-2GB).

**Total Memory Savings**: 4-7 GB (from 10GB down to 3-6GB)

```bash
#!/bin/bash
# start_hindsight_optimized.sh

# OpenAI Embeddings (1536 dimensions)
export HINDSIGHT_API_EMBEDDINGS_PROVIDER=openai
export HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY=sk-your-openai-key
export HINDSIGHT_API_EMBEDDINGS_OPENAI_MODEL=text-embedding-3-small

# FlashRank Reranking (CPU-based, tiny models)
export HINDSIGHT_API_RERANKER_PROVIDER=flashrank
export HINDSIGHT_API_RERANKER_FLASHRANK_MODEL=ms-marco-MiniLM-L-12-v2

# Reduce database connections
export HINDSIGHT_API_DB_POOL_MAX_SIZE=20

# Optional: Disable history tracking to save storage
export HINDSIGHT_API_ENABLE_MENTAL_MODEL_HISTORY=false
export HINDSIGHT_API_ENABLE_OBSERVATION_HISTORY=false

# LLM Provider (for retain/reflect operations)
export HINDSIGHT_API_LLM_PROVIDER=openai
export HINDSIGHT_API_LLM_API_KEY=sk-your-openai-key
export HINDSIGHT_API_LLM_MODEL=gpt-4o-mini

# Start Hindsight
python3 -c "
from hindsight import HindsightEmbedded
client = HindsightEmbedded(profile='hydra')
print(f'Hindsight started at: {client.url}')
print('Memory-optimized configuration active!')
input('Press Enter to stop...')
"
```

**Pros:**
- ✅ Saves 4-7 GB of memory
- ✅ Uses your existing OpenAI API key
- ✅ FlashRank is free and CPU-based
- ✅ Easy to set up

**Cons:**
- ⚠️ Increases API costs (see cost analysis below)
- ⚠️ Adds network latency (~50-100ms per embedding call)

### Option B: Cohere + FlashRank

If you prefer Cohere for embeddings (1024 dimensions):

```bash
#!/bin/bash

# Cohere Embeddings (1024 dimensions)
export HINDSIGHT_API_EMBEDDINGS_PROVIDER=cohere
export HINDSIGHT_API_EMBEDDINGS_COHERE_API_KEY=your-cohere-key
export HINDSIGHT_API_EMBEDDINGS_COHERE_MODEL=embed-english-v3.0

# FlashRank Reranking
export HINDSIGHT_API_RERANKER_PROVIDER=flashrank
export HINDSIGHT_API_RERANKER_FLASHRANK_MODEL=ms-marco-MiniLM-L-12-v2

# Rest of configuration same as Option A
```

**Pros:**
- ✅ Slightly cheaper than OpenAI
- ✅ 1024 dimensions (smaller than OpenAI's 1536)

**Cons:**
- ⚠️ Requires separate Cohere account and API key
- ⚠️ Free tier limited to 7 days

---

## Alternative Providers

### Embedding Providers

| Provider | Model | Dimensions | Pros | Cons |
|----------|-------|------------|------|------|
| **Local** (default) | `BAAI/bge-small-en-v1.5` | 384 | Free, no network latency | Uses 3-5 GB memory |
| **OpenAI** | `text-embedding-3-small` | 1536 | Fast, high quality | Costs $0.02 per 1M tokens |
| **OpenAI** | `text-embedding-3-large` | 3072 | Best quality | Costs $0.13 per 1M tokens |
| **Cohere** | `embed-english-v3.0` | 1024 | Cheaper than OpenAI | Requires separate signup |
| **TEI** | Custom | Varies | Self-hosted, fast | Requires infrastructure |

### Reranker Providers

| Provider | Model | Size | Pros | Cons |
|----------|-------|------|------|------|
| **Local** (default) | `ms-marco-MiniLM-L-6-v2` | ~34MB | Good quality | Uses CPU/GPU memory |
| **FlashRank** | `ms-marco-TinyBERT-L-2-v2` | ~4MB | Tiny, CPU-only, free | Slightly lower quality |
| **FlashRank** | `ms-marco-MiniLM-L-12-v2` | ~34MB | Best performance | Still small |
| **Cohere** | `rerank-english-v3.0` | Cloud | High quality | Requires API key |

---

## Cost Analysis

### OpenAI Embeddings Pricing

| Model | Price per 1M tokens | Typical Usage |
|-------|---------------------|---------------|
| `text-embedding-3-small` | $0.02 | 100 beads ≈ $0.10-0.50 |
| `text-embedding-3-large` | $0.13 | 100 beads ≈ $0.50-2.00 |

**Example calculation:**
- Average bead content: 1000 tokens
- 100 beads: 100,000 tokens
- Cost: 100,000 / 1,000,000 × $0.02 = **$0.002** (practically free!)

Even with thousands of beads, the cost is minimal compared to the memory savings.

### Cohere Embeddings Pricing

| Model | Price per 1M tokens |
|-------|---------------------|
| `embed-english-v3.0` | ~$0.015 (cheaper than OpenAI) |

**Note:** Cohere offers a 7-day free trial.

### Cost vs Memory Trade-off

| Setup | Memory | Monthly Cost (est.) | Best For |
|-------|--------|---------------------|----------|
| **Local (default)** | 10 GB | $0 | Development, low volume |
| **OpenAI + FlashRank** | 3-6 GB | $1-5 | Production, high volume |
| **Cohere + FlashRank** | 3-6 GB | $1-3 | Production, cost-sensitive |

---

## Troubleshooting

### Issue: Port already in use

```bash
# Find and kill existing Hindsight process
lsof -ti :9418 | xargs kill -9

# Or use the restart script
./scripts/restart_hindsight.sh
```

### Issue: Memory still high after configuration change

1. **Verify environment variables are set:**
   ```bash
   env | grep HINDSIGHT_API
   ```

2. **Restart Hindsight completely:**
   ```bash
   # Kill all Hindsight processes
   pkill -f hindsight
   
   # Wait 5 seconds
   sleep 5
   
   # Start fresh
   python3 start_hindsight.py
   ```

3. **Check current memory usage:**
   ```bash
   # Get Hindsight PID
   PID=$(pgrep -f "hindsight-api.*--port 9418" | head -1)
   
   # Get memory in MB
   ps -o rss= -p $PID | awk '{print int($1/1024) " MB"}'
   ```

### Issue: API key errors

**OpenAI:**
```bash
# Verify key format (should start with sk-)
echo $HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY | cut -c1-10

# Test with curl
curl https://api.openai.com/v1/models \
  -H "Authorization: Bearer $HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY"
```

**Cohere:**
```bash
# Test with curl
curl https://api.cohere.com/v1/embed \
  -H "Authorization: Bearer $HINDSIGHT_API_EMBEDDINGS_COHERE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"texts": ["test"], "model": "embed-english-v3.0"}'
```

### Issue: Hindsight won't start with cloud provider

1. Check logs for specific errors:
   ```bash
   # Check if Hindsight is logging errors
   tail -f /tmp/hindsight.log 2>/dev/null || echo "No log file"
   ```

2. Verify API key has credits:
   - OpenAI: Check at https://platform.openai.com/account/billing
   - Cohere: Check at https://dashboard.cohere.com/billing

3. Try the local fallback temporarily:
   ```bash
   unset HINDSIGHT_API_EMBEDDINGS_PROVIDER
   python3 start_hindsight.py
   ```

---

## Quick Reference

### Minimal Memory Configuration

```bash
# Save this as ~/.hydra-hindsight-env
export HINDSIGHT_API_EMBEDDINGS_PROVIDER=openai
export HINDSIGHT_API_EMBEDDINGS_OPENAI_API_KEY=sk-your-key
export HINDSIGHT_API_RERANKER_PROVIDER=flashrank
export HINDSIGHT_API_DB_POOL_MAX_SIZE=20
```

Then source it before starting Hindsight:
```bash
source ~/.hydra-hindsight-env
python3 start_hindsight.py
```

### Monitoring Memory Usage

```bash
# Continuous monitoring
while true; do
  PID=$(pgrep -f "hindsight-api.*--port 9418" | head -1)
  if [ -n "$PID" ]; then
    MEM=$(ps -o rss= -p $PID | awk '{print int($1/1024)}')
    echo "$(date '+%H:%M:%S') Hindsight memory: ${MEM} MB"
  fi
  sleep 30
done
```

---

## Additional Resources

- [Hindsight Official Documentation](https://hindsight.vectorize.io/)
- [Hindsight Configuration Reference](https://hindsight.vectorize.io/developer/configuration)
- [OpenAI Embeddings Documentation](https://platform.openai.com/docs/guides/embeddings)
- [FlashRank GitHub Repository](https://github.com/PrithivirajDamodaran/FlashRank)

---

**Last Updated:** March 2026  
**Related:** [Hindsight Memory Management](HINDSIGHT_MEMORY_MANAGEMENT.md) - For understanding memory usage patterns
