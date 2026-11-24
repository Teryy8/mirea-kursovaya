#!/bin/sh
set -eu

if [ "$#" -gt 1 ]; then
    echo "Usage: sh scripts/backup.sh [backup-file]" >&2
    exit 2
fi

cd "$(dirname "$0")/.."
destination=${1:-backups/analytics-$(date -u +%Y%m%dT%H%M%SZ).db}
case "$destination" in
    /*) ;;
    *) destination="./$destination" ;;
esac
directory=$(dirname "$destination")
umask 077
mkdir -p "$directory"

if [ -e "$destination" ] || [ -L "$destination" ]; then
    echo "Backup already exists: $destination" >&2
    exit 1
fi

temporary=$(mktemp "$directory/.livetraffic-backup.XXXXXXXX")
trap 'rm -f "$temporary"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker compose exec -T api /usr/local/bin/livetraffic backup > "$temporary"
test -s "$temporary"
ln "$temporary" "$destination"
printf 'Backup saved: %s\n' "$destination"
