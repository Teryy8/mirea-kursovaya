#!/bin/sh
set -eu

# Run against an empty analytics database.
base=${1:-http://127.0.0.1:8081}
event='{"event_id":"ca8f3bc5-0643-407e-80a9-6572fff5730c","type":"page_view","path":"/demo/ci-smoke"}'

curl -fsS --max-time 5 "$base/health" | jq -e '.status == "ok"' >/dev/null
curl -fsS --max-time 5 "$base/" | grep -q 'LiveTraffic'
curl -fsS --max-time 5 "$base/api/stats" | jq -e '.total_views == 0 and .pages == []' >/dev/null

post() {
    curl -sS --max-time 5 -o /dev/null -w '%{http_code}' \
        -H 'Content-Type: application/json' -d "$event" "$base/api/events"
}

first=$(post)
second=$(post)
if [ "$first" != 202 ] || [ "$second" != 200 ]; then
    printf 'Unexpected event statuses: %s, %s\n' "$first" "$second" >&2
    exit 1
fi

for _ in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS --max-time 5 "$base/api/stats" |
        jq -e '.total_views == 1 and .pages == [{"path":"/demo/ci-smoke","views":1}]' >/dev/null; then
        echo 'Smoke test passed: Nginx, event processing and deduplication.'
        exit 0
    fi
    sleep 1
done

echo 'Expected exactly one processed view.' >&2
exit 1
