#!/bin/sh
set -eu

: "${LIVETRAFFIC_APP_IMAGE:?set the Go image reference}"
: "${LIVETRAFFIC_PROXY_IMAGE:?set the Nginx image reference}"
: "${LIVETRAFFIC_DATA_NODE_ID:?set the Swarm data node ID}"
: "${LIVETRAFFIC_HEALTH_URL:?set the full URL of the proxied /health endpoint}"

stack=${LIVETRAFFIC_STACK_NAME:-livetraffic}
if [ "$#" -eq 0 ]; then
    set -- -c deploy/stack.yml
fi

# Use "never" with archives that were loaded on the target nodes beforehand.
case "${LIVETRAFFIC_RESOLVE_IMAGE:-always}" in
    always) set -- --with-registry-auth "$@" ;;
    never) set -- --resolve-image never "$@" ;;
    *) echo 'LIVETRAFFIC_RESOLVE_IMAGE must be always or never' >&2; exit 2 ;;
esac
docker stack deploy --detach=false "$@" "$stack"

check_image() {
    current=$(docker service inspect --format '{{.Spec.TaskTemplate.ContainerSpec.Image}}' "${stack}_$1")
    case "$current" in
        "$2"|"$2"@sha256:*) ;;
        *) printf '%s is running %s, expected %s\n' "$1" "$current" "$2" >&2; exit 1 ;;
    esac
}

check_image api "$LIVETRAFFIC_APP_IMAGE"
check_image worker "$LIVETRAFFIC_APP_IMAGE"
check_image nginx "$LIVETRAFFIC_PROXY_IMAGE"

health() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsS --max-time 3 "$LIVETRAFFIC_HEALTH_URL"
    else
        wget -q -O - -T 3 "$LIVETRAFFIC_HEALTH_URL"
    fi
}

for _ in 1 2 3 4 5 6 7 8 9 10; do
    if health; then
        echo
        docker stack services "$stack"
        exit 0
    fi
    sleep 1
done

echo 'The updated stack did not respond at the health URL.' >&2
docker stack ps --no-trunc "$stack" >&2
exit 1
