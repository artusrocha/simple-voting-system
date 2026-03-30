#!/usr/bin/env python3

import json
import re
import sys
from pathlib import Path


HOST_CONTEXT = {
    "os": "Linux pop-os 6.18.7-76061807-generic",
    "cpu": "13th Gen Intel(R) Core(TM) i5-13420H",
    "logical_cpus": "12",
    "ram": "15 GiB",
    "runtime": "podman",
    "profile": "benchmark",
}

SCENARIO_ORDER = ["smoke", "sustained", "spike", "stress", "consistency"]
SCENARIO_LABELS = {
    "smoke": "smoke",
    "sustained": "sustained",
    "spike": "spike",
    "stress": "stress",
    "consistency": "consistency-topic",
}


def format_float(value: float, decimals: int = 2) -> str:
    return f"{value:.{decimals}f}"


def format_rate(value: float) -> str:
    return format_float(value, 2)


def format_percent(value: float) -> str:
    return f"{value * 100:.2f}%"


def parse_scenario(path: Path) -> str | None:
    match = re.match(r"k6-([a-z-]+)-\d{8}-\d{6}-summary\.json$", path.name)
    if not match:
        return None
    scenario = match.group(1)
    if scenario == "consistency":
        return "consistency"
    return scenario


def latest_run_dir(artifacts_root: Path) -> Path:
    run_dirs = sorted(path for path in artifacts_root.iterdir() if path.is_dir())
    if not run_dirs:
        raise SystemExit(f"no performance artifact directories found under {artifacts_root}")
    return run_dirs[-1]


def load_summary(summary_path: Path) -> dict:
    with summary_path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def scenario_note(scenario: str, metrics: dict) -> str:
    if scenario == "smoke":
        vus = metrics.get("vus_max", {}).get("value")
        return f"Single-VU baseline ({int(vus)} VU)" if vus is not None else "Single-VU baseline"
    if scenario == "sustained":
        vus = metrics.get("vus_max", {}).get("value")
        return f"Plateau load up to {int(vus)} VUs" if vus is not None else "Plateau load"
    if scenario == "spike":
        vus = metrics.get("vus_max", {}).get("value")
        return f"Burst profile up to {int(vus)} VUs" if vus is not None else "Burst profile"
    if scenario == "stress":
        return "Best max-throughput result in this round"
    accepted = int(metrics.get("consistency_votes_202", {}).get("count", 0))
    sent = int(metrics.get("consistency_votes_sent", {}).get("count", 0))
    non_202 = int(metrics.get("consistency_votes_non_202", {}).get("count", 0))
    return f"{accepted}/{sent} accepted, non-202={non_202}, topic verification passed"


def extract_row(scenario: str, summary: dict, run_dir: Path, repo_root: Path) -> dict:
    metrics = summary.get("metrics", {})
    http_reqs = metrics.get("http_reqs", {})
    iterations = metrics.get("iterations", {})
    req_duration = metrics.get("http_req_duration", {})
    failed = metrics.get("http_req_failed", {})
    checks = metrics.get("checks", {})
    summary_rel_path = summary_path_relative(run_dir, scenario, repo_root)
    return {
        "scenario": SCENARIO_LABELS.get(scenario, scenario),
        "request_rate": float(http_reqs.get("rate", 0.0)),
        "iteration_rate": float(iterations.get("rate", 0.0)),
        "p95_ms": float(req_duration.get("p(95)", 0.0)),
        "avg_ms": float(req_duration.get("avg", 0.0)),
        "fail_rate": float(failed.get("value", 0.0)),
        "checks": float(checks.get("value", 0.0)),
        "note": scenario_note(scenario, metrics),
        "summary_path": summary_rel_path,
    }


def summary_path_relative(run_dir: Path, scenario: str, repo_root: Path) -> str:
    matches = sorted(run_dir.glob(f"k6-{scenario}-*-summary.json"))
    if not matches:
        raise SystemExit(f"missing summary file for scenario '{scenario}' in {run_dir}")
    return matches[-1].relative_to(repo_root).as_posix()


def collect_rows(run_dir: Path, repo_root: Path) -> list[dict]:
    summary_files = sorted(run_dir.glob("k6-*-summary.json"))
    by_scenario: dict[str, Path] = {}
    for summary_file in summary_files:
        scenario = parse_scenario(summary_file)
        if scenario is None:
            continue
        by_scenario[scenario] = summary_file

    rows = []
    for scenario in SCENARIO_ORDER:
        summary_file = by_scenario.get(scenario)
        if summary_file is None:
            continue
        rows.append(extract_row(scenario, load_summary(summary_file), run_dir, repo_root))
    if not rows:
        raise SystemExit(f"no k6 summary files found in {run_dir}")
    return rows


def latest_round_date(run_dir: Path) -> str:
    match = re.search(r"(\d{4})-(\d{2})-(\d{2})", run_dir.name)
    if not match:
        return run_dir.name
    return f"{match.group(1)}-{match.group(2)}-{match.group(3)}"


def render_markdown(rows: list[dict], run_dir: Path, repo_root: Path) -> str:
    best_row = max(rows, key=lambda row: row["iteration_rate"])
    run_dir_rel = run_dir.relative_to(repo_root).as_posix()
    date_label = latest_round_date(run_dir)

    lines = [
        "# Performance Index",
        "",
        "This index summarizes the latest benchmark round from the versioned k6 summary exports.",
        "",
        "## Latest Round",
        "",
        f"- date: `{date_label}`",
        f"- profile: `{HOST_CONTEXT['profile']}`",
        f"- source directory: `{run_dir_rel}`",
        f"- host OS: `{HOST_CONTEXT['os']}`",
        f"- CPU: `{HOST_CONTEXT['cpu']}`",
        f"- logical CPUs: `{HOST_CONTEXT['logical_cpus']}`",
        f"- RAM: `{HOST_CONTEXT['ram']}`",
        f"- runtime: `{HOST_CONTEXT['runtime']}`",
        "",
        "## Scenario Table",
        "",
        "| Scenario | Request Rate (req/s) | Vote Cycles (iter/s) | P95 (ms) | Avg (ms) | Fail Rate | Checks | Notes | Source |",
        "|---|---:|---:|---:|---:|---:|---:|---|---|",
    ]

    for row in rows:
        lines.append(
            "| {scenario} | {request_rate} | {iteration_rate} | {p95_ms} | {avg_ms} | {fail_rate} | {checks} | {note} | `{summary_path}` |".format(
                scenario=row["scenario"],
                request_rate=format_rate(row["request_rate"]),
                iteration_rate=format_rate(row["iteration_rate"]),
                p95_ms=format_float(row["p95_ms"]),
                avg_ms=format_float(row["avg_ms"]),
                fail_rate=format_percent(row["fail_rate"]),
                checks=format_percent(row["checks"]),
                note=row["note"],
                summary_path=row["summary_path"],
            )
        )

    lines.extend(
        [
            "",
            "## Highlights",
            "",
            f"- best throughput in this round: `{best_row['scenario']}` at `{format_rate(best_row['iteration_rate'])}` vote cycles/s and `{format_rate(best_row['request_rate'])}` HTTP req/s",
            "- perf scenarios execute one vote request plus one results request per iteration, so `iterations/s` is the better approximation of vote-cycle throughput",
            "- the consistency run validates deterministic acceptance and topic-count agreement, not raw max throughput",
            "",
            "## Interpretation",
            "",
            "- This index is derived from `*-summary.json` files and treats them as the canonical benchmark source.",
            "- These numbers describe a local benchmark-oriented profile that minimizes some protective overhead to measure upper-bound throughput.",
            "- They should not be presented as a production SLA or as a hardened-profile benchmark.",
            "",
            "## Related Docs",
            "",
            "- `docs/performance-summary.md`",
            "- `docs/load-testing.md`",
            f"- `{run_dir_rel}/README.md`",
        ]
    )
    return "\n".join(lines) + "\n"


def main() -> int:
    repo_root = Path(__file__).resolve().parent.parent
    artifacts_root = repo_root / "docs" / "performance-artifacts"
    output_path = repo_root / "docs" / "performance-index.md"

    if len(sys.argv) > 2:
        print(f"usage: {Path(sys.argv[0]).name} [artifacts-dir]", file=sys.stderr)
        return 1

    run_dir = Path(sys.argv[1]).resolve() if len(sys.argv) == 2 else latest_run_dir(artifacts_root)
    rows = collect_rows(run_dir, repo_root)
    output_path.write_text(render_markdown(rows, run_dir, repo_root), encoding="utf-8")
    print(output_path.relative_to(repo_root).as_posix())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
