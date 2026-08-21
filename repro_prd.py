#!/usr/bin/env python3
"""Reproduce PRD validation 'No Implementation Files' gap from actual beads.
Mimics internal/validation/prd_validator.go static check against bead DAG leaves."""
import json
from collections import defaultdict
from pathlib import Path

BEADS = Path('/opt/dev-tools/components/hydra/.beads/issues.jsonl')
PROJECT = Path('/opt/dev-tools/components/hydra')

beads = [json.loads(line) for line in BEADS.read_text().splitlines() if line.strip()]
by_id = {b['id']: b for b in beads}

# DAG edge direction matches core.DAG: OutEdges are beads that depend on this node.
dependents = defaultdict(list)  # bead_id -> list of beads that depend on it
for b in beads:
    for dep in b.get('dependencies', []):
        dependents[dep['depends_on_id']].append(b['id'])

# Roots are beads with no dependencies.
roots = [b['id'] for b in beads if not b.get('dependencies')]

def chain_members(root):
    members = {root}
    stack = [root]
    while stack:
        cur = stack.pop()
        for child in dependents[cur]:
            if child not in members:
                members.add(child)
                stack.append(child)
    return members

def leaf_ids(members):
    return [bid for bid in members if not dependents[bid]]

def target_files(bead):
    for key in ('metadata', 'bead_metadata'):
        m = bead.get(key) or {}
        tf = m.get('target_files')
        if tf:
            return [Path(x.strip()) for x in tf.split(',') if x.strip()]
    return []

def validate(root_id):
    members = chain_members(root_id)
    leaves = leaf_ids(members)
    print(f'\n--- root={root_id} members={sorted(members)} leaves={leaves} ---')
    all_files = set()
    for leaf in leaves:
        bead = by_id[leaf]
        files = target_files(bead)
        print(f'  leaf={leaf} type={bead["issue_type"]} target_files={files}')
        for f in files:
            p = (PROJECT / f).resolve() if not f.is_absolute() else f.resolve()
            if p.exists() and p.stat().st_size > 0:
                all_files.add(str(p))
            else:
                print(f'    MISSING/EMPTY: {p}')
    if not all_files:
        print('  CRITICAL: No Implementation Files - No leaf beads have target_files metadata')
        return False
    print(f'  OK: found files {all_files}')
    return True

ok = True
for root in ['hydra-04c', 'hydra-ztj', 'hydra-wqi']:
    ok &= validate(root)
raise SystemExit(0 if ok else 1)
