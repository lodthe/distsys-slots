#!/bin/sh
set -eu

# The mounted log volume survives container replacement. Keep stdout for
# `docker compose logs`, and capture stderr (including crashes) in the same file.
umask 027
log_file=$(mktemp "/logs/$(date -u +%Y%m%dT%H%M%SZ).log.XXXXXX")
chmod 640 "$log_file"
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT
mkfifo "$work_dir/output"
tee -a "$log_file" < "$work_dir/output" &
log_pid=$!

"$@" > "$work_dir/output" 2>&1 &
app_pid=$!
trap 'kill -TERM "$app_pid" 2>/dev/null || true' TERM
trap 'kill -INT "$app_pid" 2>/dev/null || true' INT

# A trapped signal interrupts wait before the child has finished shutting down.
wait_for() {
    while :; do
        if wait "$1"; then return 0; else result=$?; fi
        if ! kill -0 "$1" 2>/dev/null; then return "$result"; fi
    done
}
if wait_for "$app_pid"; then status=0; else status=$?; fi
if ! wait_for "$log_pid"; then
    printf 'Persistent log writer failed\n' >&2
    status=1
fi
exit "$status"
