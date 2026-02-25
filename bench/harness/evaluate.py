#!/usr/bin/env python3
"""Blind quality evaluation of benchmark outputs using LLM scoring."""

import json
import os
import subprocess
import sys
from pathlib import Path

RUBRIC = """Score each submission on the following criteria (1-10 each):

1. **Correctness**: Does the code work? Do tests pass? Are there runtime errors?
2. **Implementation Quality**: Code clarity, no dead code, proper error handling, clean logic
3. **Test Coverage**: Number of tests, edge cases covered, test organization
4. **Style Conformance**: Matches existing code patterns, consistent formatting, proper comments
5. **Prompt Compliance**: All requirements from the prompt are addressed

For each criterion, provide:
- Score (1-10)
- Brief justification (1 sentence)

Then provide a TOTAL out of 50 and list any deductions.

IMPORTANT: Evaluate each submission independently and objectively.
Do NOT factor in which tool produced it — both are labeled neutrally as Submission A and B.
"""


def read_diff(run_dir: str) -> str:
    """Read the git diff from a run directory."""
    for diff_file in ["diff_staged_full.patch", "diff_full.patch"]:
        path = os.path.join(run_dir, diff_file)
        if os.path.exists(path):
            with open(path) as f:
                content = f.read()
            if content.strip():
                return content
    return "(no diff captured)"


def read_output(run_dir: str) -> str:
    """Read the CLI output from a run directory."""
    for out_file in ["output.json", "output.txt"]:
        path = os.path.join(run_dir, out_file)
        if os.path.exists(path):
            with open(path) as f:
                content = f.read()
            if content.strip():
                return content[:5000]  # truncate for context window
    return "(no output captured)"


def build_eval_prompt(prompt: str, diff_a: str, diff_b: str) -> str:
    """Build the evaluation prompt with anonymized submissions."""
    return f"""You are a code review expert evaluating two independent submissions for the same coding task.

## Task Prompt
{prompt}

## Submission A — Code Changes
```diff
{diff_a}
```

## Submission B — Code Changes
```diff
{diff_b}
```

## Evaluation Rubric
{RUBRIC}

Provide your evaluation as JSON:
```json
{{
    "submission_a": {{
        "correctness": {{"score": N, "reason": "..."}},
        "implementation_quality": {{"score": N, "reason": "..."}},
        "test_coverage": {{"score": N, "reason": "..."}},
        "style_conformance": {{"score": N, "reason": "..."}},
        "prompt_compliance": {{"score": N, "reason": "..."}},
        "total": N,
        "deductions": ["..."]
    }},
    "submission_b": {{
        "correctness": {{"score": N, "reason": "..."}},
        "implementation_quality": {{"score": N, "reason": "..."}},
        "test_coverage": {{"score": N, "reason": "..."}},
        "style_conformance": {{"score": N, "reason": "..."}},
        "prompt_compliance": {{"score": N, "reason": "..."}},
        "total": N,
        "deductions": ["..."]
    }}
}}
```
"""


def evaluate_via_claude(prompt: str) -> str:
    """Call Claude CLI directly (not through proxy) for evaluation."""
    result = subprocess.run(
        ["claude", "-p", "--output-format", "json",
         "--model", "claude-sonnet-4-5-20250929",
         "--no-session-persistence"],
        input=prompt,
        capture_output=True,
        text=True,
        timeout=300,
    )
    if result.returncode != 0:
        raise RuntimeError(f"Claude CLI failed: {result.stderr}")

    # Parse Claude JSON output
    try:
        output = json.loads(result.stdout)
        return output.get("result", result.stdout)
    except json.JSONDecodeError:
        return result.stdout


def extract_json(text: str) -> dict:
    """Extract JSON from text that may contain markdown fences."""
    # Try direct parse
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass

    # Try extracting from code fences
    import re
    match = re.search(r'```(?:json)?\s*\n(.*?)\n```', text, re.DOTALL)
    if match:
        try:
            return json.loads(match.group(1))
        except json.JSONDecodeError:
            pass

    return {"raw_response": text, "parse_error": True}


def main():
    if len(sys.argv) < 2:
        print("Usage: evaluate.py <run_dir>", file=sys.stderr)
        print("  run_dir should contain scenario/repN/{claude,genesis}/ subdirectories", file=sys.stderr)
        sys.exit(1)

    run_dir = sys.argv[1]
    run_path = Path(run_dir)
    results = {}

    # Randomize which is A vs B for blind evaluation
    import random
    random.seed(42)  # reproducible

    for scenario_dir in sorted(run_path.iterdir()):
        if not scenario_dir.is_dir():
            continue
        scenario = scenario_dir.name
        results[scenario] = {}

        for rep_dir in sorted(scenario_dir.iterdir()):
            if not rep_dir.is_dir() or not rep_dir.name.startswith("rep"):
                continue

            claude_dir = rep_dir / "claude"
            genesis_dir = rep_dir / "genesis"

            if not claude_dir.is_dir() or not genesis_dir.is_dir():
                continue

            claude_diff = read_diff(str(claude_dir))
            genesis_diff = read_diff(str(genesis_dir))

            # Randomize A/B assignment
            if random.random() < 0.5:
                diff_a, diff_b = claude_diff, genesis_diff
                mapping = {"submission_a": "claude", "submission_b": "genesis"}
            else:
                diff_a, diff_b = genesis_diff, claude_diff
                mapping = {"submission_a": "genesis", "submission_b": "claude"}

            # Load prompt
            prompt_file = Path(__file__).parent / "scenarios" / f"{scenario}.txt"
            prompt = prompt_file.read_text() if prompt_file.exists() else "(prompt not found)"

            print(f"Evaluating {scenario}/{rep_dir.name}...")
            eval_prompt = build_eval_prompt(prompt, diff_a, diff_b)

            try:
                response = evaluate_via_claude(eval_prompt)
                eval_result = extract_json(response)
            except Exception as e:
                eval_result = {"error": str(e)}

            # De-anonymize
            eval_result["mapping"] = mapping
            results[scenario][rep_dir.name] = eval_result

    # Write results
    output_path = run_path / "quality_eval.json"
    with open(output_path, "w") as f:
        json.dump(results, f, indent=2)

    print(f"\nQuality evaluation written to {output_path}")

    # Print summary
    for scenario, reps in results.items():
        print(f"\n{'='*50}")
        print(f"Scenario: {scenario}")
        for rep_name, eval_data in reps.items():
            if "error" in eval_data:
                print(f"  {rep_name}: ERROR - {eval_data['error']}")
                continue
            mapping = eval_data.get("mapping", {})
            for sub_key in ["submission_a", "submission_b"]:
                tool = mapping.get(sub_key, sub_key)
                sub_data = eval_data.get(sub_key, {})
                total = sub_data.get("total", "?")
                print(f"  {rep_name} {tool}: {total}/50")


if __name__ == "__main__":
    main()
