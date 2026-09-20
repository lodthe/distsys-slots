#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_dir=$(mktemp -d)
container="distsys-log-check-$$"
volume="$container-logs"
cleanup() {
    docker rm -f "$container" >/dev/null 2>&1 || true
    docker volume rm "$volume" >/dev/null 2>&1 || true
    rm -rf "$test_dir"
}
trap cleanup EXIT
docker build -q -t distsys-log-check . > /dev/null
docker volume create "$volume" > /dev/null
run() {
    docker run "$@" --read-only --cap-drop ALL --security-opt no-new-privileges:true \
        --tmpfs /tmp:size=1m,mode=1777 \
        -v "$PWD/deploy/persistent-logs.sh:/persistent-logs.sh:ro" \
        -v "$volume:/logs" \
        --entrypoint /bin/sh distsys-log-check /persistent-logs.sh /bin/sh -c "$command"
}

read_logs() {
    docker run --rm -v "$volume:/logs:ro" alpine:3.22 "$@"
}

# Both streams, failure exit status and independent files across replacements.
command='printf "standard output\n"; printf "standard error\n" >&2; exit 7'
if run --rm > "$test_dir/console"; then
    printf 'Child failure exit status was lost\n' >&2
    exit 1
else
    test "$?" -eq 7
fi
read_logs sh -c 'cat /logs/*.log*' > "$test_dir/captured"
cmp "$test_dir/console" "$test_dir/captured"
command='printf "replacement\n"'
run --rm > /dev/null
read_logs sh -ec '
    test "$(find /logs -name "*.log*" | wc -l)" -eq 2
    grep -q "standard error" /logs/*.log*
    grep -q replacement /logs/*.log*
'

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
read_logs sh -ec '
    grep -q "shutdown complete" /logs/*.log*
    test "$(find /logs -name "*.log*" | wc -l)" -eq 3
'
printf 'PASS: persistent stdout/stderr, child exit status, recreation and graceful shutdown\n'
