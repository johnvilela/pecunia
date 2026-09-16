---
tags: [remote-access, sqlite, ssh, mcp, tailscale]
---

## Status

Recommendation recorded for later exploration. Nothing has been implemented or configured yet; Tailscale is a candidate private-network layer, not a settled dependency.

## Context

The old PC is expected to run Omni and hold Pecunia's SQLite database. The current PC may occasionally need the same financial information. Manually cloning or synchronizing the database would introduce stale copies and, if both copies were writable, conflict resolution.

Pecunia already has two useful remote-facing boundaries:

- the human CLI, which keeps all domain rules in the existing stores;
- `pecunia mcp`, a stdio MCP server exposing the same stores ([[decisions/0017-mcp-server-exposes-every-module-as-a-tool]]).

Omni already launches Pecunia locally on its host ([[decisions/0023-pecunia-is-an-omni-plugin]]), so the old PC naturally remains the data owner.

## Recommended shape

Keep exactly one authoritative SQLite database on the old PC and bring execution to the data:

```text
Phone ──Telegram──> Omni ──> pecunia ──> SQLite
                                  ▲
Current PC ──SSH/private network──┘
Current AI ──SSH + MCP stdio──────┘
```

For direct use, execute the remote binary through SSH:

```sh
ssh -t finance-pc /home/user/.local/bin/pecunia summary
ssh -t finance-pc /home/user/.local/bin/pecunia transactions
```

The `-t` allocation is useful for interactive CLI forms. A shell wrapper or alias can make this feel like a local command.

For an MCP client on the current PC, configure its local process command as SSH without a pseudo-terminal:

```json
{
  "command": "ssh",
  "args": [
    "finance-pc",
    "/home/user/.local/bin/pecunia",
    "mcp"
  ]
}
```

SSH transports the MCP stdin/stdout stream while Pecunia and SQLite continue to run on the database host. The remote command must not emit login banners or other text on stdout because stdout is the MCP protocol channel.

A private overlay such as Tailscale or WireGuard should carry SSH rather than exposing SSH or a Pecunia service directly to the public internet. Tailscale is worth evaluating later; no choice or setup has been made yet.

## Why this fits Pecunia

- There is one current source of truth and no merge algorithm.
- Both machines use the same store validations, derived values, migrations, and audit trail.
- The database and its WAL sidecars stay on local storage, preserving the concurrency and permission decisions in [[decisions/0013-data-integrity-fixes-and-known-gaps]].
- Only the old PC needs the database-compatible Pecunia version.
- Omni's phone integration keeps working exactly as designed.

The main limitation is availability: when the old PC or private connection is down, live access is unavailable.

## Important safety boundary

The current MCP server exposes writes as well as reads. If the current PC only needs to consume information, a future server-enforced `pecunia mcp --read-only` mode would be preferable to relying on prompt instructions. It could register only list/get/summary/situation/log actions or reject mutation actions at the handler boundary.

## Approaches to avoid

Do not point `PECUNIA_DB` at SSHFS, NFS, SMB, Syncthing, Dropbox, or another live network-mounted/synchronized copy. Pecunia explicitly uses WAL, a `-wal` file, a `-shm` file, locking, and a busy timeout. Network filesystems and independent file synchronization can violate SQLite's locking and write-order assumptions.

A copied database is safe only when it is created as a consistent snapshot and treated as one-way/read-only. Copying only the main `.db` file while writes may still be present in the WAL is not a sound snapshot strategy.

SQLite's own guidance recommends placing the database engine beside the database and sending API calls across the network: https://www.sqlite.org/useovernet.html

## Later options

- **First-class remote service:** If Pecunia gains several clients, add a `pecunia serve` HTTP or Streamable HTTP MCP transport. Keep the stores and SQLite on the old PC, bind privately, authenticate every caller, and separate read/write permissions. This is unnecessary infrastructure for two personal machines today.
- **Offline read snapshot:** Create a consistent one-way snapshot using SQLite's Online Backup API or `VACUUM INTO`, transfer it automatically, and open it through a true Pecunia read-only mode. `db.Open()` currently applies migrations, enables WAL, and can record time-derived bill state, so an ordinary Pecunia invocation is not a read-only snapshot consumer.
- **Continuous backup:** Litestream or periodic consistent snapshots can protect against losing the old PC. Backup is a separate concern from remote access and should not become a second writable authority.
- **Client/server database:** PostgreSQL or a similar server becomes reasonable only if Pecunia evolves into a multi-user, high-concurrency service. It would sacrifice much of the current single-file simplicity.

## Decision point for future work

Before implementing anything, establish whether the second PC needs:

1. live read-only access;
2. live reads and writes; or
3. offline reads or offline writes.

SSH remote execution is the preferred starting point for the first two. Offline writes would require a much larger synchronization design with stable cross-machine identities and explicit conflict rules; the current audit log alone is not an event-sourcing protocol.

Links: [[decisions/0013-data-integrity-fixes-and-known-gaps]] · [[decisions/0017-mcp-server-exposes-every-module-as-a-tool]] · [[decisions/0023-pecunia-is-an-omni-plugin]]