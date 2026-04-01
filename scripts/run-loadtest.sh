#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOADTEST_DIR="$PROJECT_ROOT/tests/load"

API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"
CONTROL_API_BASE_URL="${CONTROL_API_BASE_URL:-$API_BASE_URL}"
SYNC_API_URLS="${SYNC_API_URLS:-}"
VOTING_ID="${VOTING_ID:-}"
SCENARIO="${SCENARIO:-sustained}"
OUTPUT_DIR="${OUTPUT_DIR:-$PROJECT_ROOT/_/loadtest-results}"
MODE="${MODE:-perf}"
TOPIC_VERIFY_TIMEOUT_MS="${TOPIC_VERIFY_TIMEOUT_MS:-8000}"
KAFKA_CONTAINER_NAME="${KAFKA_CONTAINER_NAME:-${CONTAINER_NAME_PREFIX:-voting-platform}-kafka}"

K6_CAPTURE_RAW="${K6_CAPTURE_RAW:-false}"

K6_IMAGE="${K6_IMAGE:-docker.io/grafana/k6:latest}"
RUNTIME="${RUNTIME:-podman}"
COMPOSE_NETWORK_NAME="${COMPOSE_NETWORK_NAME:-voting-platform-network}"
LAST_OUTPUT_FILE=""
LAST_SUMMARY_FILE=""
LAST_SUMMARY_MARKDOWN_FILE=""

render_summary_report() {
    local scenario="$1"
    local summary_file="$2"
    local markdown_file="${summary_file%.json}.md"
    local rendered_text

    rendered_text=$(python3 "$PROJECT_ROOT/scripts/render-loadtest-summary.py" "$summary_file" "$scenario") || fail "Failed to render human-readable summary for $summary_file"

    if [[ ! -f "$markdown_file" ]]; then
        fail "Expected markdown summary was not created: $markdown_file"
    fi

    LAST_SUMMARY_MARKDOWN_FILE="$markdown_file"

    log "Summary saved to: $summary_file"
    log "Summary markdown saved to: $markdown_file"
    printf '\n%s\n\n' "$rendered_text"
}

log() {
    printf '[loadtest] %s\n' "$*"
}

fail() {
    printf '[loadtest][error] %s\n' "$*" >&2
    exit 1
}

mkdir -p "$OUTPUT_DIR"

wait_for_api() {
    log "Waiting for API to be ready..."
    local retries=30
    for i in $(seq 1 "$retries"); do
        if curl -sf "$API_BASE_URL/healthz" > /dev/null 2>&1; then
            log "API is ready"
            return 0
        fi
        sleep 2
    done
    fail "API did not become ready"
}

wait_for_projector() {
    local projector_url
    projector_url="${PROJECTOR_URL:-${API_BASE_URL%:*}:8081}"
    log "Waiting for projector to be ready..."
    local retries=30
    for i in $(seq 1 "$retries"); do
        if curl -sf "$projector_url/healthz" > /dev/null 2>&1; then
            log "Projector is ready"
            return 0
        fi
        sleep 2
    done
    fail "Projector did not become ready"
}

wait_for_kafka() {
    if [[ "$RUNTIME" != "podman" && "$RUNTIME" != "docker" ]]; then
        return 0
    fi

    log "Waiting for Kafka container '$KAFKA_CONTAINER_NAME' to be healthy..."
    local retries=30
    local status=""
    local health=""
    for i in $(seq 1 "$retries"); do
        status=$($RUNTIME inspect "$KAFKA_CONTAINER_NAME" --format '{{.State.Status}}' 2>/dev/null || true)
        health=$($RUNTIME inspect "$KAFKA_CONTAINER_NAME" --format '{{if .State.Healthcheck}}{{.State.Healthcheck.Status}}{{else}}unknown{{end}}' 2>/dev/null || true)
        if [[ "$status" == "running" && ( "$health" == "healthy" || "$health" == "unknown" ) ]]; then
            log "Kafka container is healthy"
            return 0
        fi
        sleep 2
    done
    fail "Kafka container '$KAFKA_CONTAINER_NAME' is not healthy (status=$status, health=$health)"
}

wait_for_voting_open() {
    local voting_id="$1"
    local targets
    if [[ -n "$SYNC_API_URLS" ]]; then
        targets="$SYNC_API_URLS"
    else
        targets="$CONTROL_API_BASE_URL"
    fi

    IFS=',' read -r -a urls <<< "$targets"
    for url in "${urls[@]}"; do
        local trimmed_url
        trimmed_url="$(printf '%s' "$url" | xargs)"
        [[ -z "$trimmed_url" ]] && continue
        local retries=30
        local state=""
        for _ in $(seq 1 "$retries"); do
            state=$(curl -sS "$trimmed_url/votings/$voting_id" | jq -r '.status // empty' 2>/dev/null || true)
            if [[ "$state" == "OPEN" ]]; then
                break
            fi
            sleep 1
        done
        if [[ "$state" != "OPEN" ]]; then
            fail "Voting $voting_id did not become OPEN on $trimmed_url"
        fi
    done
}

preflight_voting_flow() {
    log "Running benchmark preflight voting flow..."

    local voting_id
    voting_id=$(create_voting)

    local vote_status
    vote_status=$(curl -sS -o /tmp/voting-platform-loadtest-preflight-vote.json -w '%{http_code}' \
        -X POST "$API_BASE_URL/votings/$voting_id/votes" \
        -H 'Content-Type: application/json' \
        -d '{"candidateId":"c1","ip":"198.51.100.10"}')

    if [[ "$vote_status" != "202" ]]; then
        local body
        body=$(python3 - <<'PY'
from pathlib import Path
p = Path('/tmp/voting-platform-loadtest-preflight-vote.json')
print(p.read_text(encoding='utf-8') if p.exists() else '')
PY
)
        rm -f /tmp/voting-platform-loadtest-preflight-vote.json
        fail "Preflight vote failed with status $vote_status: $body"
    fi
    rm -f /tmp/voting-platform-loadtest-preflight-vote.json

    local results_status
    results_status=$(curl -sS -o /tmp/voting-platform-loadtest-preflight-results.json -w '%{http_code}' \
        "$API_BASE_URL/votings/$voting_id/results")
    if [[ "$results_status" != "200" ]]; then
        rm -f /tmp/voting-platform-loadtest-preflight-results.json
        fail "Preflight results fetch failed with status $results_status"
    fi
    rm -f /tmp/voting-platform-loadtest-preflight-results.json

    log "Benchmark preflight voting flow passed"
}

create_voting() {
    printf '[loadtest] %s\n' "Creating test voting..." >&2
    
    local response
    response=$(curl -sS -X POST "$CONTROL_API_BASE_URL/votings" \
        -H 'Content-Type: application/json' \
        -d '{
            "name": "Load Test",
            "candidates": [
                {"candidateId": "c1", "name": "Candidate 1"},
                {"candidateId": "c2", "name": "Candidate 2"}
            ]
        }')
    
    local voting_id
    voting_id=$(echo "$response" | jq -r '.votingId // empty')
    
    if [[ -z "$voting_id" ]]; then
        fail "Failed to create voting: $response"
    fi
    
    curl -sS -X PATCH "$CONTROL_API_BASE_URL/votings/$voting_id" \
        -H 'Content-Type: application/json' \
        -d '{"status":"OPEN"}' > /dev/null

    wait_for_voting_open "$voting_id"
    
    echo "$voting_id"
}

get_docker_host() {
    if [[ "$RUNTIME" == "podman" ]]; then
        echo "api"
    else
        echo "localhost"
    fi
}

run_k6() {
    local scenario="$1"
    local voting_id="$2"
    local output_file="$OUTPUT_DIR/k6-${scenario}-$(date +%Y%m%d-%H%M%S).json"
    local output_basename
    output_basename="$(basename "$output_file")"
    local summary_file="${output_file%.json}-summary.json"
    local summary_basename
    summary_basename="$(basename "$summary_file")"
    local scripts_mount="$LOADTEST_DIR:/scripts"
    local results_mount="$OUTPUT_DIR:/results"
    local k6_output_args="--summary-export /results/${summary_basename}"
    
    local docker_host
    docker_host=$(get_docker_host)

    rm -f "$output_file" "$summary_file"
    touch "$summary_file"
    chmod u+rw "$summary_file"

    if [[ "$K6_CAPTURE_RAW" == "true" ]]; then
        touch "$output_file"
        chmod u+rw "$output_file"
        k6_output_args="$k6_output_args --out json=/results/${output_basename}"
    fi
    
    log "Running k6 $scenario test..."
    log "  Voting ID: $voting_id"
    log "  Summary: $summary_file"
    if [[ "$K6_CAPTURE_RAW" == "true" ]]; then
        log "  Raw output: $output_file"
    else
        log "  Raw output: disabled (set K6_CAPTURE_RAW=true to enable)"
    fi
    
    local k6_cmd
    if [[ "$RUNTIME" == "podman" ]]; then
        k6_cmd="podman run --rm"
        k6_cmd="$k6_cmd --user 0:0"
        k6_cmd="$k6_cmd --security-opt label=disable"
        k6_cmd="$k6_cmd --network ${COMPOSE_NETWORK_NAME}"
        k6_cmd="$k6_cmd -e K6_STATSD_ENABLE=true"
        k6_cmd="$k6_cmd -e K6_STATSD_ADDR=host.docker.internal:8125"
        scripts_mount="$LOADTEST_DIR:/scripts:Z"
        results_mount="$OUTPUT_DIR:/results:Z"
    else
        k6_cmd="docker run --rm"
        k6_cmd="$k6_cmd --user $(id -u):$(id -g)"
    fi
    
    k6_cmd="$k6_cmd -v $scripts_mount"
    k6_cmd="$k6_cmd -v $results_mount"
    k6_cmd="$k6_cmd -e API_URL=http://${docker_host}:8080"
    k6_cmd="$k6_cmd -e VOTING_ID=${voting_id}"
    k6_cmd="$k6_cmd -e SCENARIO=${scenario}"
    k6_cmd="$k6_cmd -e MODE=${MODE}"
    k6_cmd="$k6_cmd -e THINK_TIME=${THINK_TIME:-0.02}"
    k6_cmd="$k6_cmd -e CONSISTENCY_ITERATIONS=${CONSISTENCY_ITERATIONS:-200}"
    k6_cmd="$k6_cmd -e CONSISTENCY_TIMEOUT_MS=${CONSISTENCY_TIMEOUT_MS:-20000}"
    k6_cmd="$k6_cmd -e CONSISTENCY_POLL_INTERVAL_MS=${CONSISTENCY_POLL_INTERVAL_MS:-250}"
    k6_cmd="$k6_cmd $K6_IMAGE"
    
    case "$scenario" in
        smoke)
            k6_cmd="$k6_cmd run $k6_output_args /scripts/k6-script.js"
            ;;
        sustained)
            k6_cmd="$k6_cmd run $k6_output_args /scripts/k6-script.js"
            ;;
        spike)
            k6_cmd="$k6_cmd run $k6_output_args /scripts/k6-script.js"
            ;;
        stress)
            k6_cmd="$k6_cmd run $k6_output_args /scripts/k6-script.js"
            ;;
        consistency)
            k6_cmd="${k6_cmd/ -e SCENARIO=${scenario}/ -e SCENARIO=consistency}"
            k6_cmd="${k6_cmd/ -e MODE=${MODE}/ -e MODE=consistency}"
            k6_cmd="$k6_cmd run $k6_output_args /scripts/k6-script.js"
            ;;
        all)
            k6_cmd="${k6_cmd/ -e SCENARIO=${scenario}/ -e SCENARIO=all}"
            k6_cmd="$k6_cmd run $k6_output_args /scripts/k6-script.js"
            ;;
    esac
    
    eval $k6_cmd

    if [[ "$K6_CAPTURE_RAW" == "true" && ! -f "$output_file" ]]; then
        fail "Expected output file was not created: $output_file"
    fi
    if [[ ! -f "$summary_file" ]]; then
        fail "Expected summary file was not created: $summary_file"
    fi

    LAST_OUTPUT_FILE=""
    if [[ "$K6_CAPTURE_RAW" == "true" ]]; then
        LAST_OUTPUT_FILE="$output_file"
    fi
    LAST_SUMMARY_FILE="$summary_file"
    LAST_SUMMARY_MARKDOWN_FILE=""

    render_summary_report "$scenario" "$summary_file"
    if [[ "$K6_CAPTURE_RAW" == "true" ]]; then
        log "Raw results saved to: $output_file"
    fi
}

verify_votes_topic() {
    local voting_id="$1"
    local summary_file="$2"
    local capture_file
    capture_file="$(mktemp)"

    local expected_count
    expected_count=$(jq -r '((.metrics.consistency_votes_202.count // 0) | floor)' "$summary_file")

    log "Verifying votes.raw topic for voting: $voting_id"
    log "  Expected events: $expected_count"

    "$RUNTIME" exec "$KAFKA_CONTAINER_NAME" kafka-console-consumer \
        --bootstrap-server localhost:9092 \
        --topic votes.raw \
        --from-beginning \
        --timeout-ms "$TOPIC_VERIFY_TIMEOUT_MS" \
        --property print.key=true \
        --property key.separator='|' > "$capture_file" 2>/dev/null || true

    local observed_count
    observed_count=$(python3 - "$capture_file" "$voting_id" <<'PY'
import json
import sys

capture_path = sys.argv[1]
voting_id = sys.argv[2]
count = 0

with open(capture_path, 'r', encoding='utf-8') as fh:
    for raw_line in fh:
        line = raw_line.strip()
        if not line or '|' not in line:
            continue
        _, payload = line.split('|', 1)
        try:
            decoded = json.loads(payload)
        except json.JSONDecodeError:
            continue
        if decoded.get('votingId') == voting_id:
            count += 1

print(count)
PY
)
    rm -f "$capture_file"

    log "  Observed events: $observed_count"

    if [[ "$observed_count" -ne "$expected_count" ]]; then
        fail "Topic verification failed: expected $expected_count events but observed $observed_count"
    fi

    log "Topic verification passed"
}

run_all_scenarios() {
    wait_for_kafka
    wait_for_api
    wait_for_projector
    preflight_voting_flow
    
    local voting_id
    if [[ -n "$VOTING_ID" ]]; then
        voting_id="$VOTING_ID"
    else
        voting_id=$(create_voting)
    fi
    
    log "Starting load test suite with voting: $voting_id"
    log "API URL: $API_BASE_URL"
    
    run_k6 smoke "$voting_id"
    run_k6 sustained "$voting_id"
    run_k6 spike "$voting_id"
    run_k6 stress "$voting_id"
    
    log "All tests completed!"
    log "Results in: $OUTPUT_DIR"
}

run_single_scenario() {
    local scenario="$1"

    wait_for_kafka
    wait_for_api
    wait_for_projector
    preflight_voting_flow

    local voting_id
    if [[ -n "$VOTING_ID" ]]; then
        voting_id="$VOTING_ID"
    else
        voting_id=$(create_voting)
    fi

    log "Starting load test scenario '$scenario' with voting: $voting_id"
    log "API URL: $API_BASE_URL"

    run_k6 "$scenario" "$voting_id"

    log "Test completed!"
    log "Results in: $OUTPUT_DIR"
}

run_consistency_with_topic_verification() {
    wait_for_kafka
    wait_for_api
    wait_for_projector
    preflight_voting_flow

    local voting_id
    if [[ -n "$VOTING_ID" ]]; then
        voting_id="$VOTING_ID"
    else
        voting_id=$(create_voting)
    fi

    log "Starting consistency + topic verification with voting: $voting_id"
    log "API URL: $API_BASE_URL"

    run_k6 consistency "$voting_id"
    verify_votes_topic "$voting_id" "$LAST_SUMMARY_FILE"

    log "Consistency + topic verification completed!"
    log "Results in: $OUTPUT_DIR"
}

usage() {
    cat <<EOF
Voting Platform Load Test Runner (using k6)

Usage: $0 [command] [options]

Commands:
    smoke       Run smoke test (1 VU, 10s)
    sustained   Run sustained load test
    spike       Run spike load test
    stress      Run stress test (900 VUs, 3min plateau)
    consistency Run correctness verification test
    consistency-topic Run correctness test plus topic verification
    all         Run all scenarios (default)
    create-voting   Create and open a voting

Options:
    API_BASE_URL    Benchmark traffic base URL (default: http://localhost:8080)
    CONTROL_API_BASE_URL Control-plane URL for create/open operations (default: API_BASE_URL)
    SYNC_API_URLS   Comma-separated API URLs that must observe OPEN state before benchmark start
    VOTING_ID       Specific voting ID to use
    SCENARIO        Test scenario to run
    OUTPUT_DIR      Output directory (default: _/loadtest-results)
    K6_IMAGE        k6 container image (default: docker.io/grafana/k6:latest)
    RUNTIME         Container runtime: podman or docker (default: podman)
    MODE            Test mode: perf or consistency (default: perf)
    TOPIC_VERIFY_TIMEOUT_MS  Consumer timeout for topic verification (default: 8000)
    THINK_TIME      Sleep between perf-mode iterations in seconds (default: 0.02)
    K6_CAPTURE_RAW  Persist raw k6 JSON output in addition to summaries (default: false)
    CONSISTENCY_ITERATIONS       Number of deterministic votes in consistency mode (default: 200)
    CONSISTENCY_TIMEOUT_MS       Materialized view convergence timeout (default: 20000)
    CONSISTENCY_POLL_INTERVAL_MS Polling interval for convergence check (default: 250)

Examples:
    # Run all tests
    $0 all

    # Run specific scenario
    SCENARIO=sustained $0

    # Use existing voting
    VOTING_ID=01ABC123 $0 sustained

    # Custom API
    API_BASE_URL=http://192.168.1.100:8080 $0 all

    # Multi-API run through benchmark LB with control-plane writes pinned to api-a
    API_BASE_URL=http://localhost:3002 CONTROL_API_BASE_URL=http://localhost:8080 SYNC_API_URLS=http://localhost:8080,http://localhost:8082 $0 stress

    # Capture raw JSON for a debugging run
    K6_CAPTURE_RAW=true $0 stress
EOF
}

case "${1:-all}" in
    smoke|sustained|spike|stress|consistency)
        run_single_scenario "$1"
        ;;
    consistency-topic)
        run_consistency_with_topic_verification
        ;;
    all)
        run_all_scenarios
        ;;
    create-voting)
        wait_for_api
        create_voting
        ;;
    -h|--help|help)
        usage
        ;;
    *)
        echo "Unknown command: $1"
        usage
        exit 1
        ;;
esac
