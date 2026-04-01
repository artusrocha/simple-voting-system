#!/usr/bin/env python3

import json
import sys
from pathlib import Path


SCENARIO_LABELS = {
    "smoke": "smoke",
    "sustained": "sustained",
    "spike": "spike",
    "stress": "stress",
    "consistency": "consistency",
}

SCENARIO_NARRATIVES = {
    "smoke": "teste smoke",
    "sustained": "teste sustentado",
    "spike": "teste de pico",
    "stress": "teste de estresse",
    "consistency": "teste de consistencia",
}


def load_summary(path: Path) -> dict:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def require_metric(metrics: dict, name: str) -> dict:
    metric = metrics.get(name)
    if not isinstance(metric, dict):
        raise SystemExit(f"missing metric '{name}'")
    return metric


def require_number(metric: dict, key: str, label: str) -> float:
    value = metric.get(key)
    if not isinstance(value, (int, float)):
        raise SystemExit(f"missing field '{key}' for metric '{label}'")
    return float(value)


def format_pt_number(value: float, decimals: int = 2) -> str:
    raw = f"{value:,.{decimals}f}"
    return raw.replace(",", "#").replace(".", ",").replace("#", ".")


def format_pt_int(value: float) -> str:
    return format_pt_number(value, 0)


def format_percent(value: float) -> str:
    return f"{format_pt_number(value * 100, 2)}%"


def format_compact_count(value: float) -> str:
    absolute = abs(value)
    if absolute >= 1_000_000:
        return f"{format_pt_number(value / 1_000_000, 2)}M"
    if absolute >= 1_000:
        return format_pt_int(value)
    return str(int(round(value)))


def format_bytes_gb(value: float) -> str:
    gb = value / 1_000_000_000
    return f"~{format_pt_number(gb, 2)} GB"


def extract_perf_metrics(metrics: dict) -> dict:
    vus = require_number(require_metric(metrics, "vus_max"), "value", "vus_max")
    reqs = require_metric(metrics, "http_reqs")
    iterations = require_metric(metrics, "iterations")
    http_p95 = require_number(require_metric(metrics, "http_req_duration"), "p(95)", "http_req_duration")
    iteration_p95 = require_number(require_metric(metrics, "iteration_duration"), "p(95)", "iteration_duration")
    fail_rate = require_number(require_metric(metrics, "http_req_failed"), "value", "http_req_failed")
    data_received = require_number(require_metric(metrics, "data_received"), "count", "data_received")
    data_sent = require_number(require_metric(metrics, "data_sent"), "count", "data_sent")
    return {
        "vus": vus,
        "req_rate": require_number(reqs, "rate", "http_reqs"),
        "req_count": require_number(reqs, "count", "http_reqs"),
        "iteration_rate": require_number(iterations, "rate", "iterations"),
        "iteration_count": require_number(iterations, "count", "iterations"),
        "http_p95": http_p95,
        "iteration_p95": iteration_p95,
        "fail_rate": fail_rate,
        "data_total": data_received + data_sent,
    }


def extract_consistency_metrics(metrics: dict) -> dict:
    base = extract_perf_metrics(metrics)
    sent = require_number(require_metric(metrics, "consistency_votes_sent"), "count", "consistency_votes_sent")
    accepted = require_number(require_metric(metrics, "consistency_votes_202"), "count", "consistency_votes_202")
    non_202 = require_number(require_metric(metrics, "consistency_votes_non_202"), "count", "consistency_votes_non_202")
    base.update({
        "sent": sent,
        "accepted": accepted,
        "non_202": non_202,
    })
    return base


def markdown_perf(scenario: str, data: dict) -> str:
    scenario_label = SCENARIO_LABELS.get(scenario, scenario)
    scenario_narrative = SCENARIO_NARRATIVES.get(scenario, f"teste {scenario}")
    lines = [
        f"### Resultados de teste de carga - {scenario_label} test com {format_pt_int(data['vus'])} VUs",
        "",
        "| Metrica | Valor |",
        "|--------|-------|",
        f"| **Usuarios Virtuais (VUs)** | {format_pt_int(data['vus'])} |",
        f"| **Taxa de Requisicoes (req/s)** | {format_pt_int(data['req_rate'])} |",
        f"| **Taxa de Ciclos de Votacao (ops/s)** | {format_pt_int(data['iteration_rate'])} |",
        f"| **Latencia P95 (HTTP)** | {format_pt_number(data['http_p95'], 2)} ms |",
        f"| **Latencia P95 (Iteracao)** | {format_pt_number(data['iteration_p95'], 2)} ms |",
        f"| **Taxa de Falha** | {format_percent(data['fail_rate'])} |",
        f"| **Total de Requisicoes** | {format_compact_count(data['req_count'])} |",
        f"| **Total de Ciclos de Votacao** | {format_compact_count(data['iteration_count'])} |",
        f"| **Dados Transferidos** | {format_bytes_gb(data['data_total'])} |",
        "",
        (
            f"O {scenario_narrative} com **{format_pt_int(data['vus'])} Usuarios Virtuais** "
            f"alcancou **{format_pt_int(data['req_rate'])} req/s** "
            f"({format_pt_int(data['iteration_rate'])} ciclos de votacao/s), com "
            f"**{format_percent(data['fail_rate'])} de falhas** e latencia P95 de "
            f"**{format_pt_number(data['http_p95'], 2)} ms**."
        ),
        "",
    ]
    return "\n".join(lines)


def markdown_consistency(data: dict) -> str:
    lines = [
        f"### Resultados de teste de carga - consistency test com {format_pt_int(data['sent'])} iteracoes",
        "",
        "| Metrica | Valor |",
        "|--------|-------|",
        f"| **Votos Enviados** | {format_pt_int(data['sent'])} |",
        f"| **Votos Aceitos (202)** | {format_pt_int(data['accepted'])} |",
        f"| **Votos Nao-202** | {format_pt_int(data['non_202'])} |",
        f"| **Taxa de Falha HTTP** | {format_percent(data['fail_rate'])} |",
        f"| **Latencia P95 (HTTP)** | {format_pt_number(data['http_p95'], 2)} ms |",
        f"| **Latencia P95 (Iteracao)** | {format_pt_number(data['iteration_p95'], 2)} ms |",
        f"| **Total de Requisicoes** | {format_compact_count(data['req_count'])} |",
        f"| **Dados Transferidos** | {format_bytes_gb(data['data_total'])} |",
        "",
        (
            f"O teste de consistencia enviou **{format_pt_int(data['sent'])} votos**, obteve "
            f"**{format_pt_int(data['accepted'])} respostas 202**, registrou "
            f"**{format_pt_int(data['non_202'])} respostas nao-202** e manteve latencia P95 HTTP de "
            f"**{format_pt_number(data['http_p95'], 2)} ms**."
        ),
        "",
    ]
    return "\n".join(lines)


def text_perf(scenario: str, data: dict) -> str:
    title = f"{SCENARIO_LABELS.get(scenario, scenario).capitalize()} test com {format_pt_int(data['vus'])} VUs"
    summary = (
        f"Resumo: o {SCENARIO_NARRATIVES.get(scenario, f'teste {scenario}')} com "
        f"{format_pt_int(data['vus'])} VUs alcancou {format_pt_int(data['req_rate'])} req/s, "
        f"{format_pt_int(data['iteration_rate'])} ciclos/s, {format_percent(data['fail_rate'])} de falhas "
        f"e P95 HTTP de {format_pt_number(data['http_p95'], 2)} ms."
    )
    return "\n".join([
        title,
        f"- req/s: {format_pt_int(data['req_rate'])}",
        f"- ciclos/s: {format_pt_int(data['iteration_rate'])}",
        f"- latencia P95 HTTP: {format_pt_number(data['http_p95'], 2)} ms",
        f"- latencia P95 iteracao: {format_pt_number(data['iteration_p95'], 2)} ms",
        f"- taxa de falha: {format_percent(data['fail_rate'])}",
        f"- total de requisicoes: {format_compact_count(data['req_count'])}",
        f"- total de ciclos: {format_compact_count(data['iteration_count'])}",
        f"- dados transferidos: {format_bytes_gb(data['data_total'])}",
        "",
        summary,
    ])


def text_consistency(data: dict) -> str:
    return "\n".join([
        f"Consistency test com {format_pt_int(data['sent'])} iteracoes",
        f"- votos enviados: {format_pt_int(data['sent'])}",
        f"- votos aceitos (202): {format_pt_int(data['accepted'])}",
        f"- votos nao-202: {format_pt_int(data['non_202'])}",
        f"- latencia P95 HTTP: {format_pt_number(data['http_p95'], 2)} ms",
        f"- latencia P95 iteracao: {format_pt_number(data['iteration_p95'], 2)} ms",
        f"- taxa de falha HTTP: {format_percent(data['fail_rate'])}",
        f"- total de requisicoes: {format_compact_count(data['req_count'])}",
        f"- dados transferidos: {format_bytes_gb(data['data_total'])}",
        "",
        (
            f"Resumo: o teste de consistencia enviou {format_pt_int(data['sent'])} votos, recebeu "
            f"{format_pt_int(data['accepted'])} respostas 202, {format_pt_int(data['non_202'])} nao-202 "
            f"e P95 HTTP de {format_pt_number(data['http_p95'], 2)} ms."
        ),
    ])


def main() -> int:
    if len(sys.argv) != 3:
        print(f"usage: {Path(sys.argv[0]).name} <summary.json> <scenario>", file=sys.stderr)
        return 1

    summary_path = Path(sys.argv[1]).resolve()
    scenario = sys.argv[2].strip().lower()
    if not summary_path.is_file():
        print(f"summary file not found: {summary_path}", file=sys.stderr)
        return 1

    summary = load_summary(summary_path)
    metrics = summary.get("metrics")
    if not isinstance(metrics, dict):
        print("summary file does not contain metrics", file=sys.stderr)
        return 1

    markdown_path = summary_path.with_suffix(".md")
    if scenario == "consistency":
        data = extract_consistency_metrics(metrics)
        markdown = markdown_consistency(data)
        text = text_consistency(data)
    else:
        data = extract_perf_metrics(metrics)
        markdown = markdown_perf(scenario, data)
        text = text_perf(scenario, data)

    markdown_path.write_text(markdown, encoding="utf-8")
    print(text)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
