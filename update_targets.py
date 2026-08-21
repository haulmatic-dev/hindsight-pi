#!/usr/bin/env python3
"""Backfill target_files metadata on leaf beads for PRD validation."""
import json
from pathlib import Path
from datetime import datetime, timezone

BEADS = Path('/opt/dev-tools/components/hydra/.beads/issues.jsonl')

# Map bead_id -> target_files string. Values are relative to /opt/dev-tools/components/hydra.
TARGETS = {
    'hydra-mlb': 'internal/core/config_manager.go',
    'hydra-vtu': 'internal/core/config_manager.go',
    'hydra-wqi': '.beads/issues.jsonl,CHANGELOG.md',
    'hydra-l1n': '.beads/issues.jsonl,CHANGELOG.md',
}

lines = BEADS.read_text().splitlines(keepends=True)
out = []
updated = []
for line in lines:
    if not line.strip():
        out.append(line)
        continue
    bead = json.loads(line)
    bid = bead['id']
    if bid in TARGETS:
        if bead.get('metadata') is None:
            bead['metadata'] = {}
        if bead.get('bead_metadata') is None:
            bead['bead_metadata'] = {}
        bead['metadata']['target_files'] = TARGETS[bid]
        bead['bead_metadata']['target_files'] = TARGETS[bid]
        bead['updated_at'] = datetime.now(timezone.utc).isoformat()
        updated.append(bid)
    out.append(json.dumps(bead, ensure_ascii=False) + '\n')

BEADS.write_text(''.join(out))
print('updated', updated)
