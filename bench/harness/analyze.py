#!/usr/bin/env python3
"""Analyze benchmark traffic JSONL files and produce summary statistics."""

import json
import os
import sys
from dataclasses import dataclass, field, asdict
from pathlib import Path
from typing import Optional


# Anthropic pricing ($/M tokens) — Opus 4.6
PRICING = {
    "claude-opus-4-6": {
        "input": 15.0,
        "output": 75.0,
        "cache_write": 18.75,
        "cache_read": 1.875,
    },
    # Sonnet 4.5 for reference
    "claude-sonnet-4-5-20250929": {
        "input": 3.0,
        "output": 15.0,
        "cache_write": 3.75,
        "cache_read": 0.30,
    },
}


@dataclass
class RunMetrics:
    tool: str = ""
    scenario: str = ""
    rep: int = 0
    total_calls: int = 0
    input_tokens: int = 0
    output_tokens: int = 0
    cache_creation_tokens: int = 0
    cache_read_tokens: int = 0
    total_tokens: int = 0
    request_bytes: int = 0
    response_bytes: int = 0
    total_latency_ms: int = 0
    elapsed_ms: int = 0
    model: str = ""
    cost_usd: float = 0.0
    per_call: list = field(default_factory=list)


@dataclass
class Comparison:
    scenario: str = ""
    reps: int = 0
    claude: Optional[RunMetrics] = None
    genesis: Optional[RunMetrics] = None
    token_ratio: float = 0.0
    cost_ratio: float = 0.0
    latency_ratio: float = 0.0
    input_token_ratio: float = 0.0
    output_token_ratio: float = 0.0
    call_ratio: float = 0.0
    token_savings_pct: float = 0.0
    cost_savings_pct: float = 0.0


@dataclass
class StatResult:
    """Statistical comparison across multiple reps."""
    metric: str = ""
    claude_mean: float = 0.0
    claude_std: float = 0.0
    genesis_mean: float = 0.0
    genesis_std: float = 0.0
    ratio: float = 0.0
    p_value: float = 0.0
    cohens_d: float = 0.0
    significant: bool = False


def load_traffic(jsonl_path: str) -> list[dict]:
    """Parse a JSONL traffic file into a list of records."""
    records = []
    with open(jsonl_path) as f:
        for line in f:
            line = line.strip()
            if line:
                records.append(json.loads(line))
    return records


def aggregate(records: list[dict], tool: str = "", scenario: str = "", rep: int = 0) -> RunMetrics:
    """Sum up token counts, costs, and latency from traffic records."""
    m = RunMetrics(tool=tool, scenario=scenario, rep=rep)

    for rec in records:
        m.total_calls += 1
        m.input_tokens += rec.get("input_tokens", 0)
        m.output_tokens += rec.get("output_tokens", 0)
        m.cache_creation_tokens += rec.get("cache_creation_input_tokens", 0)
        m.cache_read_tokens += rec.get("cache_read_input_tokens", 0)
        m.request_bytes += rec.get("request_bytes", 0)
        m.response_bytes += rec.get("response_bytes", 0)
        m.total_latency_ms += rec.get("latency_ms", 0)
        if rec.get("model"):
            m.model = rec["model"]

        m.per_call.append({
            "seq": rec.get("call_seq", 0),
            "input_tokens": rec.get("input_tokens", 0),
            "output_tokens": rec.get("output_tokens", 0),
            "latency_ms": rec.get("latency_ms", 0),
        })

    m.total_tokens = m.input_tokens + m.output_tokens
    m.cost_usd = compute_cost(m)
    return m


def compute_cost(m: RunMetrics, pricing: dict = None) -> float:
    """Apply Anthropic pricing to compute USD cost."""
    if pricing is None:
        pricing = PRICING.get(m.model, PRICING.get("claude-opus-4-6"))

    cost = 0.0
    # Standard input tokens (excluding cache)
    standard_input = m.input_tokens - m.cache_creation_tokens - m.cache_read_tokens
    if standard_input > 0:
        cost += (standard_input / 1_000_000) * pricing["input"]

    cost += (m.output_tokens / 1_000_000) * pricing["output"]
    cost += (m.cache_creation_tokens / 1_000_000) * pricing["cache_write"]
    cost += (m.cache_read_tokens / 1_000_000) * pricing["cache_read"]

    return round(cost, 6)


def safe_div(a: float, b: float) -> float:
    return a / b if b != 0 else 0.0


def compare(claude: RunMetrics, genesis: RunMetrics, scenario: str = "") -> Comparison:
    """Compare two run metrics and compute ratios."""
    c = Comparison(scenario=scenario)
    c.claude = claude
    c.genesis = genesis
    c.token_ratio = safe_div(claude.total_tokens, genesis.total_tokens)
    c.input_token_ratio = safe_div(claude.input_tokens, genesis.input_tokens)
    c.output_token_ratio = safe_div(claude.output_tokens, genesis.output_tokens)
    c.cost_ratio = safe_div(claude.cost_usd, genesis.cost_usd)
    c.latency_ratio = safe_div(claude.total_latency_ms, genesis.total_latency_ms)
    c.call_ratio = safe_div(claude.total_calls, genesis.total_calls)

    c.token_savings_pct = (1 - safe_div(genesis.total_tokens, claude.total_tokens)) * 100
    c.cost_savings_pct = (1 - safe_div(genesis.cost_usd, claude.cost_usd)) * 100

    return c


def statistical_compare(claude_runs: list[RunMetrics], genesis_runs: list[RunMetrics],
                         metric: str = "total_tokens") -> StatResult:
    """Welch's t-test and Cohen's d across reps. Requires scipy."""
    result = StatResult(metric=metric)

    claude_vals = [getattr(r, metric) for r in claude_runs]
    genesis_vals = [getattr(r, metric) for r in genesis_runs]

    import statistics
    result.claude_mean = statistics.mean(claude_vals)
    result.genesis_mean = statistics.mean(genesis_vals)

    if len(claude_vals) >= 2:
        result.claude_std = statistics.stdev(claude_vals)
    if len(genesis_vals) >= 2:
        result.genesis_std = statistics.stdev(genesis_vals)

    result.ratio = safe_div(result.claude_mean, result.genesis_mean)

    # Statistical tests require >= 3 reps
    if len(claude_vals) >= 3 and len(genesis_vals) >= 3:
        try:
            from scipy.stats import ttest_ind
            t_stat, p_val = ttest_ind(claude_vals, genesis_vals, equal_var=False)
            result.p_value = p_val
            result.significant = p_val < 0.05

            # Cohen's d
            pooled_std = ((result.claude_std ** 2 + result.genesis_std ** 2) / 2) ** 0.5
            if pooled_std > 0:
                result.cohens_d = (result.claude_mean - result.genesis_mean) / pooled_std
        except ImportError:
            pass  # scipy not available

    return result


def find_traffic_files(run_dir: str) -> dict:
    """Discover JSONL traffic files organized by scenario/rep/tool."""
    results = {}
    run_path = Path(run_dir)

    for scenario_dir in sorted(run_path.iterdir()):
        if not scenario_dir.is_dir() or scenario_dir.name in ("meta.json",):
            continue
        scenario = scenario_dir.name
        results[scenario] = {}

        for rep_dir in sorted(scenario_dir.iterdir()):
            if not rep_dir.is_dir() or not rep_dir.name.startswith("rep"):
                continue
            rep_num = int(rep_dir.name.replace("rep", ""))
            results[scenario][rep_num] = {}

            for tool_dir in sorted(rep_dir.iterdir()):
                if not tool_dir.is_dir():
                    continue
                tool = tool_dir.name
                jsonl_files = list(tool_dir.glob("*.jsonl"))
                elapsed_file = tool_dir / "elapsed_ms.txt"
                elapsed_ms = 0
                if elapsed_file.exists():
                    try:
                        elapsed_ms = int(elapsed_file.read_text().strip())
                    except ValueError:
                        pass

                results[scenario][rep_num][tool] = {
                    "jsonl_files": [str(f) for f in jsonl_files],
                    "elapsed_ms": elapsed_ms,
                    "dir": str(tool_dir),
                }

    return results


def analyze_run_dir(run_dir: str) -> dict:
    """Analyze all traffic in a benchmark run directory."""
    structure = find_traffic_files(run_dir)

    analysis = {
        "run_dir": run_dir,
        "scenarios": {},
    }

    # Load metadata if available
    meta_path = Path(run_dir) / "meta.json"
    if meta_path.exists():
        with open(meta_path) as f:
            analysis["meta"] = json.load(f)

    for scenario, reps in structure.items():
        scenario_data = {
            "comparisons": [],
            "claude_runs": [],
            "genesis_runs": [],
        }

        for rep_num, tools in sorted(reps.items()):
            claude_metrics = None
            genesis_metrics = None

            for tool_name, tool_data in tools.items():
                all_records = []
                for jsonl_path in tool_data["jsonl_files"]:
                    all_records.extend(load_traffic(jsonl_path))

                metrics = aggregate(all_records, tool=tool_name, scenario=scenario, rep=rep_num)
                metrics.elapsed_ms = tool_data["elapsed_ms"]

                if tool_name == "claude":
                    claude_metrics = metrics
                    scenario_data["claude_runs"].append(asdict(metrics))
                elif tool_name == "genesis":
                    genesis_metrics = metrics
                    scenario_data["genesis_runs"].append(asdict(metrics))

            if claude_metrics and genesis_metrics:
                comp = compare(claude_metrics, genesis_metrics, scenario=scenario)
                comp.reps = rep_num
                scenario_data["comparisons"].append({
                    "rep": rep_num,
                    "token_ratio": round(comp.token_ratio, 2),
                    "input_token_ratio": round(comp.input_token_ratio, 2),
                    "output_token_ratio": round(comp.output_token_ratio, 2),
                    "cost_ratio": round(comp.cost_ratio, 2),
                    "latency_ratio": round(comp.latency_ratio, 2),
                    "call_ratio": round(comp.call_ratio, 2),
                    "token_savings_pct": round(comp.token_savings_pct, 1),
                    "cost_savings_pct": round(comp.cost_savings_pct, 1),
                    "claude_tokens": claude_metrics.total_tokens,
                    "genesis_tokens": genesis_metrics.total_tokens,
                    "claude_cost": claude_metrics.cost_usd,
                    "genesis_cost": genesis_metrics.cost_usd,
                    "claude_calls": claude_metrics.total_calls,
                    "genesis_calls": genesis_metrics.total_calls,
                })

        # Statistical comparison if multiple reps
        claude_runs_obj = []
        genesis_runs_obj = []
        for rep_num, tools in sorted(reps.items()):
            for tool_name, tool_data in tools.items():
                records = []
                for p in tool_data["jsonl_files"]:
                    records.extend(load_traffic(p))
                m = aggregate(records, tool=tool_name, scenario=scenario, rep=rep_num)
                m.elapsed_ms = tool_data["elapsed_ms"]
                if tool_name == "claude":
                    claude_runs_obj.append(m)
                elif tool_name == "genesis":
                    genesis_runs_obj.append(m)

        if len(claude_runs_obj) >= 2 and len(genesis_runs_obj) >= 2:
            stats = {}
            for metric in ["total_tokens", "input_tokens", "output_tokens", "cost_usd", "elapsed_ms"]:
                sr = statistical_compare(claude_runs_obj, genesis_runs_obj, metric)
                stats[metric] = asdict(sr)
            scenario_data["statistics"] = stats

        analysis["scenarios"][scenario] = scenario_data

    return analysis


def main():
    if len(sys.argv) < 2:
        print("Usage: analyze.py <run_dir>", file=sys.stderr)
        sys.exit(1)

    run_dir = sys.argv[1]
    if not os.path.isdir(run_dir):
        print(f"Error: {run_dir} is not a directory", file=sys.stderr)
        sys.exit(1)

    # Optional pricing override
    pricing_override = None
    if len(sys.argv) >= 3 and sys.argv[2] == "--pricing":
        pricing_file = sys.argv[3]
        with open(pricing_file) as f:
            pricing_override = json.load(f)

    analysis = analyze_run_dir(run_dir)

    output_path = os.path.join(run_dir, "analysis.json")
    with open(output_path, "w") as f:
        json.dump(analysis, f, indent=2)

    print(f"Analysis written to {output_path}")

    # Print quick summary
    for scenario, data in analysis.get("scenarios", {}).items():
        print(f"\n{'='*60}")
        print(f"Scenario: {scenario}")
        for comp in data.get("comparisons", []):
            print(f"  Rep {comp['rep']}: "
                  f"Claude {comp['claude_tokens']:,} tokens (${comp['claude_cost']:.3f}) "
                  f"vs Genesis {comp['genesis_tokens']:,} tokens (${comp['genesis_cost']:.3f}) "
                  f"— {comp['token_ratio']:.1f}x ratio, "
                  f"{comp['token_savings_pct']:.1f}% savings")


if __name__ == "__main__":
    main()
