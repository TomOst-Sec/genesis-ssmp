#!/usr/bin/env python3
"""Generate markdown benchmark report from analysis.json."""

import json
import sys
from pathlib import Path


def fmt_tokens(n: int) -> str:
    if n >= 1_000_000:
        return f"{n/1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n/1_000:.1f}K"
    return str(n)


def fmt_cost(c: float) -> str:
    return f"${c:.3f}"


def fmt_pct(p: float) -> str:
    return f"{p:+.1f}%"


def fmt_ratio(r: float) -> str:
    return f"{r:.1f}x"


def fmt_ms(ms: int) -> str:
    if ms >= 60_000:
        return f"{ms/60_000:.1f}m"
    if ms >= 1_000:
        return f"{ms/1_000:.1f}s"
    return f"{ms}ms"


def generate_report(analysis: dict) -> str:
    """Generate a full markdown report."""
    lines = []
    meta = analysis.get("meta", {})

    lines.append("# Genesis vs Claude CLI — Benchmark Report")
    lines.append("")
    if meta:
        lines.append(f"**Date:** {meta.get('timestamp', 'N/A')}")
        lines.append(f"**Model:** {meta.get('model', 'N/A')}")
        lines.append(f"**Reps per scenario:** {meta.get('reps', 'N/A')}")
        lines.append("")

    lines.append("---")
    lines.append("")

    # Table 1: Primary Token Comparison
    lines.append("## Table 1: Primary Token Comparison")
    lines.append("")
    lines.append("| Scenario | Claude Tokens | Genesis Tokens | Ratio | Savings | Target |")
    lines.append("|----------|---------------|----------------|-------|---------|--------|")

    for scenario, data in analysis.get("scenarios", {}).items():
        for comp in data.get("comparisons", []):
            target = "3x" if comp["token_ratio"] < 5.0 else "5x"
            status = "PASS" if comp["token_ratio"] >= 3.0 else "FAIL"
            lines.append(
                f"| {scenario} (rep{comp['rep']}) "
                f"| {fmt_tokens(comp['claude_tokens'])} "
                f"| {fmt_tokens(comp['genesis_tokens'])} "
                f"| {fmt_ratio(comp['token_ratio'])} "
                f"| {fmt_pct(-comp['token_savings_pct'])} "
                f"| {target} {status} |"
            )
    lines.append("")

    # Table 2: Token Attribution Breakdown
    lines.append("## Table 2: Token Attribution Breakdown")
    lines.append("")
    lines.append("| Scenario | Tool | Input | Output | Cache Write | Cache Read | Total |")
    lines.append("|----------|------|-------|--------|-------------|------------|-------|")

    for scenario, data in analysis.get("scenarios", {}).items():
        for run_list, tool_name in [(data.get("claude_runs", []), "Claude"),
                                     (data.get("genesis_runs", []), "Genesis")]:
            for run in run_list:
                lines.append(
                    f"| {scenario} R{run.get('rep', '?')} "
                    f"| {tool_name} "
                    f"| {fmt_tokens(run.get('input_tokens', 0))} "
                    f"| {fmt_tokens(run.get('output_tokens', 0))} "
                    f"| {fmt_tokens(run.get('cache_creation_tokens', 0))} "
                    f"| {fmt_tokens(run.get('cache_read_tokens', 0))} "
                    f"| {fmt_tokens(run.get('total_tokens', 0))} |"
                )
    lines.append("")

    # Table 3: Cost Breakdown
    lines.append("## Table 3: Cost Breakdown")
    lines.append("")
    lines.append("| Scenario | Claude Cost | Genesis Cost | Ratio | Savings |")
    lines.append("|----------|------------|-------------|-------|---------|")

    for scenario, data in analysis.get("scenarios", {}).items():
        for comp in data.get("comparisons", []):
            lines.append(
                f"| {scenario} (rep{comp['rep']}) "
                f"| {fmt_cost(comp['claude_cost'])} "
                f"| {fmt_cost(comp['genesis_cost'])} "
                f"| {fmt_ratio(comp['cost_ratio'])} "
                f"| {fmt_pct(-comp['cost_savings_pct'])} |"
            )
    lines.append("")

    # Table 4: Latency & Calls
    lines.append("## Table 4: Latency & API Calls")
    lines.append("")
    lines.append("| Scenario | Claude Calls | Genesis Calls | Call Ratio | Latency Ratio |")
    lines.append("|----------|-------------|---------------|------------|---------------|")

    for scenario, data in analysis.get("scenarios", {}).items():
        for comp in data.get("comparisons", []):
            lines.append(
                f"| {scenario} (rep{comp['rep']}) "
                f"| {comp['claude_calls']} "
                f"| {comp['genesis_calls']} "
                f"| {fmt_ratio(comp['call_ratio'])} "
                f"| {fmt_ratio(comp['latency_ratio'])} |"
            )
    lines.append("")

    # Table 5: Per-Turn Accumulation (if per_call data available)
    has_per_call = False
    for scenario, data in analysis.get("scenarios", {}).items():
        for run in data.get("claude_runs", []) + data.get("genesis_runs", []):
            if run.get("per_call"):
                has_per_call = True
                break

    if has_per_call:
        lines.append("## Table 5: Per-Turn Token Accumulation")
        lines.append("")
        lines.append("| Scenario | Tool | Turn | Input Tokens | Output Tokens | Cumulative |")
        lines.append("|----------|------|------|-------------|---------------|-----------|")

        for scenario, data in analysis.get("scenarios", {}).items():
            for run_list, tool_name in [(data.get("claude_runs", []), "Claude"),
                                         (data.get("genesis_runs", []), "Genesis")]:
                for run in run_list:
                    cumulative = 0
                    for call in run.get("per_call", []):
                        call_total = call.get("input_tokens", 0) + call.get("output_tokens", 0)
                        cumulative += call_total
                        lines.append(
                            f"| {scenario} R{run.get('rep', '?')} "
                            f"| {tool_name} "
                            f"| {call.get('seq', '?')} "
                            f"| {fmt_tokens(call.get('input_tokens', 0))} "
                            f"| {fmt_tokens(call.get('output_tokens', 0))} "
                            f"| {fmt_tokens(cumulative)} |"
                        )
        lines.append("")

    # Table 6: Cross-Scenario Summary
    if len(analysis.get("scenarios", {})) > 1:
        lines.append("## Table 6: Cross-Scenario Comparison")
        lines.append("")
        lines.append("| Scenario | Token Ratio | Cost Ratio | Call Ratio | Verdict |")
        lines.append("|----------|------------|-----------|-----------|---------|")

        for scenario, data in analysis.get("scenarios", {}).items():
            comps = data.get("comparisons", [])
            if comps:
                avg_token_ratio = sum(c["token_ratio"] for c in comps) / len(comps)
                avg_cost_ratio = sum(c["cost_ratio"] for c in comps) / len(comps)
                avg_call_ratio = sum(c["call_ratio"] for c in comps) / len(comps)
                verdict = "PASS" if avg_token_ratio >= 3.0 else "FAIL"
                lines.append(
                    f"| {scenario} "
                    f"| {fmt_ratio(avg_token_ratio)} "
                    f"| {fmt_ratio(avg_cost_ratio)} "
                    f"| {fmt_ratio(avg_call_ratio)} "
                    f"| {verdict} |"
                )
        lines.append("")

    # Table 7: Statistical Significance
    has_stats = False
    for scenario, data in analysis.get("scenarios", {}).items():
        if "statistics" in data:
            has_stats = True
            break

    if has_stats:
        lines.append("## Table 7: Statistical Significance")
        lines.append("")
        lines.append("| Scenario | Metric | Claude (mean +/- std) | Genesis (mean +/- std) | Ratio | p-value | Cohen's d | Sig? |")
        lines.append("|----------|--------|----------------------|----------------------|-------|---------|-----------|------|")

        for scenario, data in analysis.get("scenarios", {}).items():
            stats = data.get("statistics", {})
            for metric_name, sr in stats.items():
                lines.append(
                    f"| {scenario} "
                    f"| {metric_name} "
                    f"| {sr['claude_mean']:.0f} +/- {sr['claude_std']:.0f} "
                    f"| {sr['genesis_mean']:.0f} +/- {sr['genesis_std']:.0f} "
                    f"| {fmt_ratio(sr['ratio'])} "
                    f"| {sr['p_value']:.4f} "
                    f"| {sr['cohens_d']:.2f} "
                    f"| {'Yes' if sr['significant'] else 'No'} |"
                )
        lines.append("")

    # Table 8: Projected Savings at Scale
    lines.append("## Table 8: Projected Savings at Scale")
    lines.append("")
    lines.append("| Scale | Claude Monthly | Genesis Monthly | Savings/Month | Savings/Year |")
    lines.append("|-------|---------------|----------------|---------------|-------------|")

    # Use first comparison for projection
    sample_comp = None
    for scenario, data in analysis.get("scenarios", {}).items():
        comps = data.get("comparisons", [])
        if comps:
            sample_comp = comps[0]
            break

    if sample_comp:
        claude_per_run = sample_comp["claude_cost"]
        genesis_per_run = sample_comp["genesis_cost"]
        for scale_label, runs_per_month in [("10 runs/day", 300), ("50 runs/day", 1500),
                                              ("100 runs/day", 3000), ("500 runs/day", 15000)]:
            claude_monthly = claude_per_run * runs_per_month
            genesis_monthly = genesis_per_run * runs_per_month
            savings_monthly = claude_monthly - genesis_monthly
            savings_yearly = savings_monthly * 12
            lines.append(
                f"| {scale_label} "
                f"| {fmt_cost(claude_monthly)} "
                f"| {fmt_cost(genesis_monthly)} "
                f"| {fmt_cost(savings_monthly)} "
                f"| {fmt_cost(savings_yearly)} |"
            )
    lines.append("")

    # Footer
    lines.append("---")
    lines.append("")
    lines.append("*Generated by Genesis benchmark harness*")

    return "\n".join(lines)


def main():
    if len(sys.argv) < 2:
        print("Usage: report.py <analysis.json>", file=sys.stderr)
        sys.exit(1)

    analysis_path = sys.argv[1]
    with open(analysis_path) as f:
        analysis = json.load(f)

    report = generate_report(analysis)

    # Write report next to analysis.json
    output_dir = Path(analysis_path).parent
    report_path = output_dir / "report.md"
    report_path.write_text(report)
    print(f"Report written to {report_path}")

    # Also print to stdout
    print(report)


if __name__ == "__main__":
    main()
