# Voting Platform Load Test Suite

Load and stress testing using [k6](https://k6.io/).

## Quick Start

```bash
# Create a test voting
./scripts/run-loadtest.sh create-voting

# Run all load tests
./scripts/run-loadtest.sh all

# Run specific scenario
VOTING_ID=01ABC123 ./scripts/run-loadtest.sh sustained

# Run black-box consistency verification
./scripts/run-loadtest.sh consistency

# Run black-box + topic verification
./scripts/run-loadtest.sh consistency-topic
```

## Requirements

- Docker or Podman
- k6 image (`docker.io/grafana/k6:latest`)

## Usage

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `API_URL` | `http://localhost:8080` | API base URL |
| `API_BASE_URL` | `http://localhost:8080` | API URL used by runner health checks and voting creation; set `http://localhost:3002` to route through the benchmark LB |
| `VOTING_ID` | auto-created | Voting ID to test |
| `OUTPUT_DIR` | `_/loadtest-results` | Results output directory |
| `K6_IMAGE` | `docker.io/grafana/k6:latest` | k6 container image |
| `RUNTIME` | `podman` | Container runtime (`podman` or `docker`) |
| `THINK_TIME` | `0.02` | Sleep between iterations in perf mode |
| `K6_CAPTURE_RAW` | `false` | Persist raw k6 JSON in addition to summaries |
| `CONSISTENCY_ITERATIONS` | `200` | Deterministic vote count in consistency mode |
| `CONSISTENCY_TIMEOUT_MS` | `20000` | Max wait for materialized results to converge |
| `CONSISTENCY_POLL_INTERVAL_MS` | `250` | Poll interval for materialized results |
| `TOPIC_VERIFY_TIMEOUT_MS` | `8000` | Kafka consumer timeout for topic verification |

### Commands

```bash
# Create a new test voting
./scripts/run-loadtest.sh create-voting

# Run smoke test (1 VU, 10s)
./scripts/run-loadtest.sh smoke

# Run sustained load test
./scripts/run-loadtest.sh sustained

# Run spike load test  
./scripts/run-loadtest.sh spike

# Run stress test
./scripts/run-loadtest.sh stress

# Run stress test through the benchmark LB
API_BASE_URL=http://localhost:3002 CONTROL_API_BASE_URL=http://localhost:8080 SYNC_API_URLS=http://localhost:8080,http://localhost:8082 ./scripts/run-loadtest.sh stress

# Run stress test (900 VUs, 3-minute plateau)
./scripts/run-loadtest.sh stress

# Run consistency test (black-box)
./scripts/run-loadtest.sh consistency

# Run consistency test + votes.raw verification (white-box)
./scripts/run-loadtest.sh consistency-topic

# Run all scenarios
./scripts/run-loadtest.sh all
```

## Direct k6 Usage

```bash
# With Docker
docker run --rm -v $(pwd)/tests/load:/scripts \
  -e API_URL=http://localhost:8080 \
  -e VOTING_ID=your-voting-id \
  grafana/k6 run /scripts/k6-script.js

# With Podman
podman run --rm -v $(pwd)/tests/load:/scripts \
  -e API_URL=http://host.docker.internal:8080 \
  -e VOTING_ID=your-voting-id \
  docker.io/grafana/k6 run /scripts/k6-script.js
```

## Test Scenarios

The k6 script includes these scenarios:

1. **smoke** - 1 VU for 10s (baseline)
2. **sustained** - 50 VUs for 2min (normal sustained load)
3. **spike** - 5→200→5 VUs (traffic spike)
4. **stress** - 900 VUs ramp with 3-minute plateau (best performer)
5. **consistency** - deterministic 200-vote run with strict 202 + materialized convergence checks

## Benchmark Modes

- direct API (`http://localhost:8080`) measures per-node API throughput because the runner talks to a single API instance
- benchmark LB (`http://localhost:3002`) distributes traffic across `api` and `api-b` without the frontend edge rate limit; create/open operations stay pinned to the control API until both replicas observe `OPEN`
- frontend edge (`http://localhost:3000/api`) is production-like, but its rate limiting can intentionally cap vote throughput

## Verification Modes

- **Black-box (`consistency`)**: verifies API acceptance (`202`) and eventual materialized consistency (`GET /results`).
- **White-box (`consistency-topic`)**: does black-box verification plus direct `votes.raw` topic count validation for the test `votingId`.

## Metrics

- **Throughput**: requests/second
- **Latency**: p95, p99 percentiles
- **Error Rate**: failed requests percentage

## Results

Results are printed to stdout and written by the runner to `_/loadtest-results`.

By default, the runner stores:

- k6 summary JSON (`k6-<scenario>-<timestamp>-summary.json`)

When `K6_CAPTURE_RAW=true`, the runner also stores:

- raw k6 output JSON (`k6-<scenario>-<timestamp>.json`)

This repository treats the summary JSON files as the canonical benchmark evidence. Use raw JSON only for short-lived debugging runs.

For the report-facing resource sizing recommendation derived from the `stress` sample, see `docs/performance-summary.md`.

You can still run k6 directly with:

```bash
podman run --rm -v $(pwd)/tests/load:/scripts \
  -e API_URL=http://host.docker.internal:8080 \
  -e VOTING_ID=your-voting-id \
  docker.io/grafana/k6 run -o json=results.json /scripts/k6-script.js
```
