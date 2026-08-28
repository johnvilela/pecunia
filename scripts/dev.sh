#!/bin/sh
# Build the dev CLI: dev, wired to a seeded database at the repo root.
#
# ponytail: the database path is baked in with -ldflags, not read from the
# environment, so a dev binary can never open the real database — PECUNIA_DB is
# ignored by design. Unstripped and untrimmed too, unlike build.sh, so stack
# traces stay readable.
#
# Usage: ./scripts/dev.sh [--reseed]   (--reseed throws the database away first)
set -e

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DB="$ROOT/pecunia.dev.db"
LDFLAGS="-X pecunia/internal/db.DevDB=$DB"

if [ "$1" = "--reseed" ]; then
	rm -f "$DB" "$DB-wal" "$DB-shm"
	echo "removed $DB"
fi

CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$ROOT/dev" "$ROOT/cmd"

# The seeder gets the same baked-in path, so it cannot wander off either.
go run -ldflags="$LDFLAGS" "$ROOT/scripts/seed"

echo "built: $ROOT/dev (database: $DB)"
