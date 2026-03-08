# Deployment Guide

## Production Deployment Checklist

### Pre-Deployment

- [ ] Build binaries for target architecture
- [ ] Create configuration file
- [ ] Set up monitoring (Prometheus/Grafana)
- [ ] Configure log aggregation
- [ ] Set up alerting

### Infrastructure

- [ ] Provision server (2 CPU, 4GB RAM minimum)
- [ ] Open port 8080 (API)
- [ ] Set up reverse proxy (nginx/caddy)
- [ ] Configure SSL/TLS
- [ ] Set up firewall rules

### Security

- [ ] Create dedicated `hydra` user
- [ ] Set file permissions (600 for config)
- [ ] Enable API authentication
- [ ] Configure rate limiting
- [ ] Set up audit logging

### Monitoring

- [ ] Deploy Prometheus
- [ ] Import Hydra dashboard
- [ ] Set up alerts:
  - [ ] High error rate
  - [ ] Worker failures
  - [ ] Cycle detection
  - [ ] API latency

### Backup

- [ ] Schedule config backups
- [ ] Document recovery procedure
- [ ] Test restore process

## Rollback Procedure

1. Stop Hydra: `sudo systemctl stop hydra`
2. Restore previous binary
3. Restore previous config
4. Start Hydra: `sudo systemctl start hydra`
5. Verify: `curl http://localhost:8080/health`

## Scaling

### Horizontal Scaling

For high-volume workloads:

1. Deploy multiple Hydra instances
2. Use shared state store (Redis/etcd)
3. Load balance across instances
4. Partition projects by instance

### Vertical Scaling

Increase resources:

- **CPU**: More workers (`max_workers`)
- **Memory**: Larger DAG cache
- **Disk**: Log retention

## Maintenance Windows

### Daily

- Review logs for errors
- Check worker success rate
- Monitor API latency

### Weekly

- Review cycle detections
- Analyze retry patterns
- Update dependencies

### Monthly

- Performance review
- Capacity planning
- Security updates

## Emergency Contacts

- On-call engineer: [TBD]
- Infrastructure team: [TBD]
- Product owner: [TBD]