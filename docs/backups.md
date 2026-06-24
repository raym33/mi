# Backups and Integrity

`mi` keeps cooperative accounting state on disk. This is **cooperative accounting, not a trustless payment rail** — the hash chains make tampering detectable, but they cannot recover a file that was deleted or lost. Back the state up.

## State files (city-mode defaults)

| File | Contents |
| --- | --- |
| `data/mi-city.db` | SQLite/WAL: consumers, providers, API key/token hashes, quotas, usage. |
| `data/mi-settlement.db` | SQLite/WAL: hash-chained settlement events (debits and provider rewards). |
| `data/challenge-chain.jsonl` | Append-only benchmark challenge ledger. |

SQLite databases may have `-wal` and `-shm` sidecar files. Back up the whole set so a restore is consistent. The exact paths come from your coordinator config (`city.sqlite_path`, `settlement.sqlite_path`, `challenges.path`).

## Backing up

```bash
make backup
# or, with custom locations:
DATA_DIR=/srv/mi/data BACKUP_DIR=/srv/mi/backups bash scripts/backup-state.sh
```

This writes a timestamped `mi-state-<timestamp>.tar.gz` containing the three files and any SQLite sidecars.

For a quiescent backup, stop the coordinator first (or take a SQLite online backup). A tarball of a live WAL database is usually restorable, but stopping the process is the safe option for archival copies.

## Integrity anchor

The coordinator exposes a combined integrity manifest at `GET /admin/integrity`. Its `anchor.anchor_hash` binds the settlement ledger and the challenge ledger into a single value. Publishing that hash externally (a timestamping service, a transparency log, an email to members) gives tamper-evident proof of the accounting state at a point in time.

```bash
make anchor-hash
# or:
ADMIN_TOKEN=admin-dev-token bash scripts/anchor-hash.sh
```

You can also verify the chains directly:

```bash
curl -fsS http://localhost:8080/admin/settlement/verify -H "Authorization: Bearer $ADMIN_TOKEN"
curl -fsS http://localhost:8080/admin/challenges/verify -H "Authorization: Bearer $ADMIN_TOKEN"
```
