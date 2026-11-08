> **DEPRECATED (2026-11-08):** this guide predates the unified memory system. GPU claims below are wrong for this host (no GPU; embeddings/reranker run CPU-only). Current truth: `docs/operations/memory-system.md` (architecture, pipelines, quality harness) and the infra repo `services/hindsight/` (config + runbooks).

# Hindsight Memory Management Guide

## Overview

Hindsight uses significant GPU memory (7-10 GB) due to loaded ML/embedding models. This is **expected behavior** for ML workloads, not a memory leak. The memory is used for:
- Embedding model weights (loaded into GPU)
- Embedding cache for fast retrieval
- Python runtime and libraries

## Why Memory Stays High After Completion

Even after all beads are processed, Hindsight retains GPU memory because:
1. **ML models stay loaded** for fast subsequent queries
2. **Embedding cache** accumulates to improve performance
3. **GPU memory is not released** until process termination

This is by design for production ML services - unloading/reloading models would be too slow.

## Solutions

### 1. Quick Fix: Restart After Project Completion (Recommended)

Run this script after Hydra completes all beads:

```bash
./scripts/restart_hindsight.sh
```

This will:
- Stop the current Hindsight process
- Free all GPU memory
- Start a fresh Hindsight instance
- Memory usage will drop from ~10 GB to ~500 MB

### 2. Automatic Monitoring

Run the monitor script to automatically restart when memory exceeds threshold:

```bash
# Monitor and restart if memory exceeds 8 GB (default)
./scripts/monitor_hindsight.sh

# Custom threshold: restart if memory exceeds 6 GB
./scripts/monitor_hindsight.sh 6144

# Custom threshold and check interval (check every 30 seconds)
./scripts/monitor_hindsight.sh 6144 30
```

### 3. Manual Restart

If you want to restart manually:

```bash
# Find Hindsight PID
ps aux | grep hindsight-api

# Kill it
kill -9 <PID>

# Restart
python3 start_hindsight.py
```

### 4. Integrated Lifecycle Manager (Future Enhancement)

The codebase now includes `internal/services/hindsight_lifecycle.go` which can:
- Monitor Hindsight memory usage
- Automatically restart after idle timeout
- Restart when memory exceeds threshold
- Track health status

To enable this in Hydra, the overseer needs to:
1. Start Hindsight via the lifecycle manager instead of externally
2. Route all Hindsight calls through the manager
3. Allow the manager to restart when needed

**Note**: Currently, Hindsight is started externally via `start_hindsight.py`, so the lifecycle manager monitors but doesn't control the process. For full integration, Hydra would need to start Hindsight itself.

## Memory Expectations

| State | Memory Usage | Notes |
|-------|-------------|-------|
| Fresh start | ~500 MB | Python + minimal loading |
| After queries | 2-4 GB | Models + cache warming |
| After heavy use | 7-10 GB | Full model cache |
| After restart | ~500 MB | Back to fresh state |

## Trade-offs

**Keeping Hindsight Running:**
- ✅ Fast queries (no model reloading)
- ✅ Maintains embedding cache
- ❌ Uses 7-10 GB GPU memory permanently

**Restarting After Completion:**
- ✅ Frees GPU memory for other tasks
- ✅ Fresh start for next project
- ❌ First queries will be slower (cache miss)
- ❌ Brief downtime during restart (~5 seconds)

## Recommendation

**For Development/Single Projects:**
- Let Hindsight run during development
- Restart after project completion to free memory

**For Production/Continuous Operation:**
- Keep Hindsight running for performance
- Monitor with `monitor_hindsight.sh` if memory is constrained
- Schedule periodic restarts during low-traffic periods

## Troubleshooting

**Hindsight won't start after restart:**
```bash
# Check if port is in use
lsof -i :9418

# Kill any lingering processes
lsof -ti :9418 | xargs kill -9

# Then restart
python3 start_hindsight.py
```

**Memory still high after restart:**
- Check for zombie Python processes: `ps aux | grep hindsight`
- Kill all related processes before restarting
- Verify GPU memory cleared: `nvidia-smi` (if using CUDA)

**Check current memory usage:**
```bash
# Get Hindsight PID
PID=$(pgrep -f "hindsight-api.*--port 9418" | head -1)

# Get memory in MB
ps -o rss= -p $PID | awk '{print int($1/1024) " MB"}'
```
