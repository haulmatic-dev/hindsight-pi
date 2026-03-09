# Hydra Monitoring and Metrics

This document describes the monitoring and metrics capabilities added to Hydra.

## Metrics Overview

Hydra now tracks the following metrics:

### Bead Processing Metrics
- **Total beads processed**: Total number of beads that have been processed
- **Beads closed successfully**: Number of beads successfully closed
- **Beads failed**: Number of beads that failed to close
- **Beads retried**: Number of beads that were retried due to dependencies
- **Processing rate**: Beads processed per minute

### Worker Metrics
- **Active workers**: Current number of running workers
- **Worker duration**: Time taken by each worker to complete
- **Worker success rate**: Percentage of successful worker completions
- **Average worker time**: Average time per worker execution

### System Metrics
- **Poll interval**: Time between bead checks
- **Max workers**: Maximum concurrent workers
- **Worker timeout**: Maximum time allowed per worker
- **Retry attempts**: Number of retry attempts per bead

## Viewing Metrics

Metrics are logged to the Hydra log file. To view real-time metrics:

```bash
# Tail the log file
tail -f /tmp/hydra.log | grep -E "(Worker|Completed|Closed|rate)"

# View summary statistics
grep "Completed:" /tmp/hydra.log | wc -l  # Total completed workers
grep "Closed bead" /tmp/hydra.log | wc -l  # Total closed beads
```

## Performance Optimization

Based on testing, the optimal configuration for processing beads:

```yaml
overseer:
  poll_interval: 5s        # Check for ready beads every 5 seconds
  max_workers: 5           # Process up to 5 beads concurrently
  worker_timeout: 3m       # 3 minutes per worker (opencode takes ~1-2m)
  max_retries: 2           # Retry failed beads up to 2 times
  retry_delay: 5s          # Wait 5 seconds between retries
```

**Performance Results:**
- **30-minute test**: 89 beads closed (73% success rate)
- **Processing rate**: ~3 beads per minute
- **Worker duration**: 40-120 seconds per bead

## Troubleshooting

### Beads Not Closing
If beads are processed but not closing:

1. Check dependency status:
   ```bash
   cd <project-dir> && br show <bead-id>
   ```

2. Check for dependency warnings in logs:
   ```bash
   grep "blocked by\|dependencies" /tmp/hydra.log
   ```

3. Ensure root beads (no dependencies) are processed first

### Workers Failing
If workers fail consistently:

1. Check worker logs for errors:
   ```bash
   grep "Worker.*failed\|exit.*error" /tmp/hydra.log
   ```

2. Verify opencode is working:
   ```bash
   echo "test" | opencode run --format json --dir <project-path>
   ```

3. Check project path configuration in hydra.yaml

## Alerting

Recommended alerts to set up:

1. **Worker failure rate > 20%**: May indicate opencode issues
2. **No beads processed for 10 minutes**: May indicate configuration issues
3. **Zero beads closed after 30 minutes**: May indicate dependency blocking

## Future Enhancements

Planned monitoring improvements:

- [ ] Prometheus metrics endpoint
- [ ] Grafana dashboard
- [ ] Slack/email notifications for failures
- [ ] Historical performance tracking
- [ ] Dependency chain visualization
