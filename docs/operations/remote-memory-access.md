# Remote Memory Access — SSH Tunnel + MCP Server

**Status:** current as of 2026-11-08 · **Audience:** team members on laptops + operators
**Parent doc:** `docs/operations/memory-system.md`

How a remote dev (laptop) reads/writes the shared `coding-brain` memory bank over an ordinary SSH connection — for pi users, Claude Code users, and any MCP client.

---

## 1. Architecture (why SSH, not an exposed API)

```
laptop                                   server (hermes-vm)
┌────────────────────────┐               ┌──────────────────────────┐
│ pi + hindsight-pi ext ─┼─ssh -L 8888──►│ hindsight-api            │
│ Claude Code (MCP)     ─┤   (tunnel)    │ bound 127.0.0.1:8888     │
│ Cursor (MCP)          ─┤               │ bank: coding-brain       │
└────────────────────────┘               └──────────────────────────┘
```

- **Auth = your existing SSH key.** No shared API key, no new credentials, no new
  network listener. The server never exposes the bank beyond localhost; the SSH
  tunnel carries it to you encrypted.
- **Attribution**: sshd logs record who connected; enable bank audit log for
  mutation records (operator hardening step, §5).
- **Mosh cannot replace this** — mosh has no port forwarding. Use plain
  `ssh -L` (the `LocalForward` block below makes it automatic). Mosh stays fine
  for terminals alongside it.

## 2. One-time laptop setup

### 2a. SSH tunnel (all users)

Append to `~/.ssh/config` (or let `setup-remote-memory.sh` do it idempotently):

```
Host hermes-vm
  HostName <server-hostname-or-ip>
  LocalForward 8888 127.0.0.1:8888
  ExitOnForwardFailure yes
```

From then on, any `ssh hermes-vm` brings the memory tunnel up alongside your
shell. For an always-on tunnel independent of a shell, run
`autossh -N hermes-vm` (or a user systemd service).

### 2b. pi users

```bash
pi install git:github.com/haulmatic-dev/hindsight-pi
mkdir -p ~/.hindsight
cat > ~/.hindsight/config.json <<'EOF'
{
  "baseUrl": "http://127.0.0.1:8888",
  "bankId": "coding-brain",
  "bankStrategy": "manual"
}
EOF
```

Identical bytes to the on-server config — the tunnel makes remote laptops look
local. Recall runs per turn; retains queue on local disk and flush with
`async:true` at session boundaries (laptop sleep/offline is safe: the queue is
durable and retries; pi keeps running memory-less via the fail-open timeout
guards while the tunnel is down).

### 2c. Claude Code / Cursor / any MCP client

The MCP server is **already running on the server** — it is a feature of
`hindsight-api`, not something you install. You only point your client at the
URL (through the tunnel):

```bash
claude mcp add hindsight -- http://127.0.0.1:8888/mcp/coding-brain/
```

(Streamable HTTP + SSE transport; 36 tools: recall, retain, reflect, mental
models, documents, knowledge-base tree/search, operations.) In Cursor or other
MCP clients, add the same URL as an MCP server entry. When the server upgrades,
every client gets the new tools automatically — no local versions to track.

## 3. Day-2 behavior

| Situation | What happens |
|---|---|
| Tunnel down / laptop offline | pi: memory silently off (status `offline`), work continues; queued retains flush on reconnect. MCP clients: tools fail fast — reconnect tunnel |
| Slow link | 5s connect timeout + per-call timeout races bound every call; no hangs |
| Server restart | Durable operations resume; nothing lost client-side |

## 4. What you do NOT need on a laptop

- **No `cm` (CASS)** — the bank already contains the curated rules
  (`source:cass` facts, nightly sync); recall serves them. `cm` remains a
  server-side tool (run it over your ssh session if you curate rules).
- **No MCP server install** — see §2c.
- **No TLS config** — the tunnel encrypts; localhost HTTP is correct inside it.

## 5. Operator checklist (before first laptop connects)

1. **Bank hardening** (REQUIRED, queued): `mcp_enabled_tools` allowlist on
   `coding-brain` excluding destructive tools (`clear_memories`, `delete_bank`,
   `clear_mental_model`, `delete_document`) + `audit_log_enabled: true`, then
   restart the service. Tunnel users are SSH-authenticated at connect time, but
   the application layer sees only localhost — the allowlist bounds what a
   compromised laptop could do.
2. Verify sshd `AllowTcpForwarding yes`.
3. Probe from a laptop: `curl -s http://127.0.0.1:8888/health` (tunnel up),
   then a recall/retain round-trip in pi and `tools/list` via the MCP client.

## 6. FAQ

- **"Install the MCP server from npm?"** No — it ships inside `hindsight-api`
  on the server. Clients reference a URL.
- **"Why not expose 8888 with an API key?"** Kept as a fallback design
  (ApiKeyTenantExtension) for future non-SSH paths (CI elsewhere, third-party
  tools); not needed while every user has SSH access.
- **"Does my git push change?"** No — this is memory transport only and does
  not touch the PAT-based publish-identity model (see `host-setup.md` §11).
