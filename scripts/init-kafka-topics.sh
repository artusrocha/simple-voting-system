#!/usr/bin/env bash
set -euo pipefail

BOOTSTRAP_SERVER="kafka:9092"
TOPICS_MANIFEST="${TOPICS_MANIFEST:-/contracts/topics/topics.yaml}"

create_topic() {
  local name="$1"
  shift
  local attempt output status

  for attempt in 1 2 3 4 5; do
    set +e
    output=$(kafka-topics \
      --bootstrap-server "$BOOTSTRAP_SERVER" \
      --create \
      --if-not-exists \
      --topic "$name" \
      "$@" 2>&1)
    status=$?
    set -e

    if [[ $status -eq 0 ]]; then
      printf '%s\n' "$output"
      return 0
    fi

    if [[ $attempt -lt 5 ]]; then
      printf '[kafka-init] retrying topic %s (attempt %s/5)\n' "$name" "$attempt" >&2
      sleep 2
      continue
    fi

    printf '%s\n' "$output" >&2
    return "$status"
  done
}

parse_topics_manifest() {
  python3 - "$TOPICS_MANIFEST" <<'PY'
import sys

path = sys.argv[1]
topics = []
current = None
in_config = False

with open(path, "r", encoding="utf-8") as fh:
    for raw_line in fh:
        line = raw_line.rstrip("\n")
        stripped = line.strip()

        if not stripped or stripped.startswith("#") or stripped == "topics:":
            continue

        if line.startswith("  - "):
            if current is not None:
                topics.append(current)
            current = {"config": {}}
            in_config = False
            key, value = stripped[2:].split(":", 1)
            current[key.strip()] = value.strip()
            continue

        if current is None:
            continue

        if stripped == "config:":
            in_config = True
            continue

        key, value = stripped.split(":", 1)
        key = key.strip()
        value = value.strip()

        if in_config and line.startswith("      "):
            current["config"][key] = value
        else:
            in_config = False
            current[key] = value

if current is not None:
    topics.append(current)

for topic in topics:
    configs = ";".join(f"{key}={value}" for key, value in topic["config"].items())
    print("\t".join([
        topic["name"],
        topic["partitions"],
        topic["replicationFactor"],
        configs,
    ]))
PY
}

while IFS=$'\t' read -r name partitions replication_factor configs; do
  args=(--partitions "$partitions" --replication-factor "$replication_factor")

  if [[ -n "$configs" ]]; then
    IFS=';' read -ra config_items <<< "$configs"
    for item in "${config_items[@]}"; do
      [[ -n "$item" ]] || continue
      args+=(--config "$item")
    done
  fi

  create_topic "$name" "${args[@]}"
done < <(parse_topics_manifest)

kafka-topics --bootstrap-server "$BOOTSTRAP_SERVER" --list
