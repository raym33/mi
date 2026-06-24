#!/usr/bin/env bash
set -euo pipefail

# Back up the coordinator's persistent state to a timestamped tarball.
#
# The coordinator keeps cooperative state in three files (city-mode defaults):
#   - data/mi-city.db         SQLite/WAL: consumers, providers, quotas, usage
#   - data/mi-settlement.db   SQLite/WAL: hash-chained settlement events
#   - data/challenge-chain.jsonl  append-only benchmark challenge ledger
#
# This is cooperative accounting, not a payment rail. Back it up regularly:
# the hash chains detect tampering, but they cannot recover a deleted file.
#
# Usage:
#   bash scripts/backup-state.sh                 # backs up ./data to ./backups
#   DATA_DIR=/srv/mi/data BACKUP_DIR=/srv/mi/backups bash scripts/backup-state.sh

DATA_DIR="${DATA_DIR:-data}"
BACKUP_DIR="${BACKUP_DIR:-backups}"

if [ ! -d "$DATA_DIR" ]; then
  echo "data directory '$DATA_DIR' not found" >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR"
stamp="$(date +%Y%m%d-%H%M%S)"
archive="$BACKUP_DIR/mi-state-$stamp.tar.gz"

# Include WAL/SHM sidecar files if present so SQLite restores cleanly.
tar -czf "$archive" -C "$DATA_DIR" \
  $(cd "$DATA_DIR" && ls mi-city.db* mi-settlement.db* challenge-chain.jsonl 2>/dev/null)

echo "wrote $archive"
