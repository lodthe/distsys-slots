#!/bin/sh
set -eu
umask 077
backup_once() {
    backup_name="distsys-$(date -u +%Y%m%dT%H%M%SZ).dump"
    if [ -e "/backups/${backup_name}" ]; then
        printf 'Backup already exists for this second\n' >&2
        return 1
    fi
    if ! pg_dump --format=custom --no-owner --file="/backups/${backup_name}.partial"; then
        return 1
    fi
    mv "/backups/${backup_name}.partial" "/backups/${backup_name}"
    printf 'Backup created: %s\n' "$backup_name"
}
if [ "${1:-loop}" = "once" ]; then
    backup_once
    exit 0
fi
trap 'exit 0' TERM INT
while :; do
    if ! backup_once; then
        printf 'Backup failed\n' >&2
    fi
    sleep 86400 &
    wait $! || true
done
