#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d)
container="distsys-log-check-$$"
cleanup() {
    docker rm -f "$container" >/dev/null 2>&1 || true
    rm -rf "$test_dir"
}
trap cleanup EXIT
mkdir "$test_dir/logs"
chmod 2770 "$test_dir/logs"
run() {
    docker run "$@" --read-only --user 10001:10001 --group-add "$(id -g)" \
        --tmpfs /tmp:size=1m,mode=1777 \
        -v "$PWD/deploy/persistent-logs.sh:/persistent-logs.sh:ro" \
        -v "$test_dir/logs:/logs" \
        --entrypoint /bin/sh alpine:3.22 /persistent-logs.sh /bin/sh -c "$command"
}

# Both streams, failure exit status and independent files across replacements.
command='printf "standard output\n"; printf "standard error\n" >&2; exit 7'
if run --rm > "$test_dir/console"; then
    printf 'Child failure exit status was lost\n' >&2
    exit 1
else
    test "$?" -eq 7
fi
cmp "$test_dir/console" "$test_dir"/logs/*.log*
command='printf "replacement\n"'
run --rm > /dev/null
test "$(find "$test_dir/logs" -name '*.log*' | wc -l)" -eq 2
grep -q 'standard error' "$test_dir"/logs/*.log*
grep -q 'replacement' "$test_dir"/logs/*.log*

# Stop must reach the application and wait for its final output before exiting.
command='trap '\''printf "stopping\n"; sleep 1; printf "shutdown complete\n"; exit 0'\'' TERM; printf "ready\n"; while :; do sleep 1; done'
run -d --name "$container" > /dev/null
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if docker logs "$container" | grep -q ready; then break; fi
    sleep 1
done
docker logs "$container" | grep -q ready
docker stop --time 10 "$container" > /dev/null
test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" -eq 0
grep -q 'shutdown complete' "$test_dir"/logs/*.log*
test "$(find "$test_dir/logs" -name '*.log*' | wc -l)" -eq 3
printf 'PASS: persistent stdout/stderr, child exit status, recreation and graceful shutdown\n'
