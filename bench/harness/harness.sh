#!/usr/bin/env bash
set -euo pipefail

# ── Genesis vs Claude CLI Benchmark Harness ──
# Usage: bash harness.sh <scenario> [reps]
# Example: bash harness.sh s1_star_quantifier 3

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Config ──
SCENARIO="${1:-s1_star_quantifier}"
REPS="${2:-1}"
COOLDOWN="${BENCH_COOLDOWN:-30}"
PROXY_PORT="${BENCH_PROXY_PORT:-9999}"
HEAVEN_PORT="${BENCH_HEAVEN_PORT:-4444}"
REPO_URL="${BENCH_REPO_URL:-https://github.com/TomOst-Sec/tinygrep.git}"
BENCH_DIR="${BENCH_DIR:-/tmp/bench}"
GENESIS_BIN="${GENESIS_BIN:-/home/tom/Work/UNIVERSE/genesis/genesis}"
GENESIS_CLI="${GENESIS_CLI:-/home/tom/Work/UNIVERSE/genesis/genesis-cli}"
PROXY_BIN="${SCRIPT_DIR}/proxy/proxy"
MODEL="${BENCH_MODEL:-claude-opus-4-6}"

# PIDs for cleanup
PROXY_PID=""
HEAVEN_PID=""

# ── Logging ──
log() { echo "[harness] $(date +%H:%M:%S) $*"; }
die() { log "FATAL: $*"; cleanup; exit 1; }

# ── Cleanup ──
cleanup() {
    stop_proxy
    stop_heaven
}
trap cleanup EXIT

# ── Proxy lifecycle ──
start_proxy() {
    local tool="$1" run_id="$2" output_dir="$3"
    log "Starting proxy (tool=$tool run=$run_id)"
    "$PROXY_BIN" \
        --listen ":${PROXY_PORT}" \
        --target "https://api.anthropic.com" \
        --output-dir "$output_dir" \
        --run-id "$run_id" \
        --tool "$tool" \
        &
    PROXY_PID=$!
    wait_proxy
}

stop_proxy() {
    if [[ -n "$PROXY_PID" ]] && kill -0 "$PROXY_PID" 2>/dev/null; then
        log "Stopping proxy (PID=$PROXY_PID)"
        kill "$PROXY_PID" 2>/dev/null || true
        wait "$PROXY_PID" 2>/dev/null || true
    fi
    PROXY_PID=""
}

wait_proxy() {
    log "Waiting for proxy to be ready..."
    local attempts=0
    while ! curl -sf "http://localhost:${PROXY_PORT}/health" >/dev/null 2>&1; do
        attempts=$((attempts + 1))
        if [[ $attempts -ge 30 ]]; then
            die "Proxy failed to start after 30 attempts"
        fi
        sleep 0.2
    done
    log "Proxy ready"
}

# ── Heaven lifecycle ──
start_heaven() {
    local data_dir="$1"
    log "Starting Heaven server (addr=127.0.0.1:${HEAVEN_PORT} data=$data_dir)"
    mkdir -p "$data_dir"
    "$GENESIS_BIN" serve \
        --addr "127.0.0.1:${HEAVEN_PORT}" \
        --data-dir "$data_dir" \
        &
    HEAVEN_PID=$!

    # Wait for Heaven to be ready
    local attempts=0
    while ! curl -sf "http://127.0.0.1:${HEAVEN_PORT}/health" >/dev/null 2>&1; do
        attempts=$((attempts + 1))
        if [[ $attempts -ge 30 ]]; then
            die "Heaven failed to start after 30 attempts"
        fi
        sleep 0.2
    done
    log "Heaven ready"
}

stop_heaven() {
    if [[ -n "$HEAVEN_PID" ]] && kill -0 "$HEAVEN_PID" 2>/dev/null; then
        log "Stopping Heaven (PID=$HEAVEN_PID)"
        kill "$HEAVEN_PID" 2>/dev/null || true
        wait "$HEAVEN_PID" 2>/dev/null || true
    fi
    HEAVEN_PID=""
}

# ── Repository management ──
clone_repo() {
    local dest="$1"
    log "Cloning tinygrep -> $dest"
    git clone --depth 1 "$REPO_URL" "$dest" 2>/dev/null
}

# ── Diff capture ──
capture_diff() {
    local run_dir="$1"
    local repo_dir="$run_dir/repo"
    (
        cd "$repo_dir"
        git diff --stat > "$run_dir/diff_stat.txt" 2>/dev/null || true
        git diff --numstat > "$run_dir/diff_numstat.txt" 2>/dev/null || true
        git diff > "$run_dir/diff_full.patch" 2>/dev/null || true
        # Include untracked files
        git add -A 2>/dev/null || true
        git diff --cached --stat > "$run_dir/diff_staged_stat.txt" 2>/dev/null || true
        git diff --cached --numstat > "$run_dir/diff_staged_numstat.txt" 2>/dev/null || true
        git diff --cached > "$run_dir/diff_staged_full.patch" 2>/dev/null || true
    )
}

# ── Syntax check ──
syntax_check() {
    local run_dir="$1"
    local repo_dir="$run_dir/repo"
    log "Running syntax checks..."
    local errors=0
    while IFS= read -r -d '' pyfile; do
        if ! python3 -m py_compile "$pyfile" 2>"$run_dir/syntax_errors.txt"; then
            log "SYNTAX ERROR: $pyfile"
            errors=$((errors + 1))
        fi
    done < <(find "$repo_dir" -name "*.py" -print0 2>/dev/null)
    echo "$errors" > "$run_dir/syntax_error_count.txt"
}

# ── Claude CLI run ──
run_claude() {
    local run_dir="$1" prompt="$2" run_id="$3"
    log "=== Running Claude CLI (run=$run_id) ==="

    clone_repo "$run_dir/repo"

    # Write MISSION.md for consistency
    echo "$prompt" > "$run_dir/repo/MISSION.md"

    # Start proxy for Claude
    start_proxy "claude" "$run_id" "$run_dir"

    local start_time
    start_time=$(date +%s%N)

    # Invoke Claude CLI
    ANTHROPIC_BASE_URL="http://localhost:${PROXY_PORT}" \
    claude -p --output-format json \
        --model "$MODEL" \
        --no-session-persistence \
        --dangerously-skip-permissions \
        "$prompt" \
        --cwd "$run_dir/repo" \
        > "$run_dir/output.json" 2> "$run_dir/stderr.log" || true

    local end_time
    end_time=$(date +%s%N)
    local elapsed_ms=$(( (end_time - start_time) / 1000000 ))
    echo "$elapsed_ms" > "$run_dir/elapsed_ms.txt"

    stop_proxy

    capture_diff "$run_dir"
    syntax_check "$run_dir"

    log "Claude CLI done (${elapsed_ms}ms)"
}

# ── Genesis CLI run ──
run_genesis() {
    local run_dir="$1" prompt="$2" run_id="$3"
    log "=== Running Genesis CLI (run=$run_id) ==="

    clone_repo "$run_dir/repo"

    # Write MISSION.md
    echo "$prompt" > "$run_dir/repo/MISSION.md"

    # Start proxy for Genesis
    start_proxy "genesis" "$run_id" "$run_dir"

    # Start Heaven server
    local heaven_data="$run_dir/heaven_data"
    start_heaven "$heaven_data"

    # Index the repo
    log "Indexing repo..."
    "$GENESIS_BIN" index "$run_dir/repo" \
        --addr "127.0.0.1:${HEAVEN_PORT}" 2>/dev/null || true

    local start_time
    start_time=$(date +%s%N)

    # Run mission
    ANTHROPIC_BASE_URL="http://localhost:${PROXY_PORT}" \
    "$GENESIS_CLI" --run-mission --cwd "$run_dir/repo" \
        > "$run_dir/output.txt" 2> "$run_dir/stderr.log" || true

    local end_time
    end_time=$(date +%s%N)
    local elapsed_ms=$(( (end_time - start_time) / 1000000 ))
    echo "$elapsed_ms" > "$run_dir/elapsed_ms.txt"

    stop_heaven
    stop_proxy

    capture_diff "$run_dir"
    syntax_check "$run_dir"

    log "Genesis CLI done (${elapsed_ms}ms)"
}

# ── Load scenario prompt ──
load_prompt() {
    local scenario="$1"
    local prompt_file="${SCRIPT_DIR}/scenarios/${scenario}.txt"
    if [[ ! -f "$prompt_file" ]]; then
        die "Scenario file not found: $prompt_file"
    fi
    cat "$prompt_file"
}

# ── List available scenarios ──
list_scenarios() {
    echo "Available scenarios:"
    for f in "$SCRIPT_DIR"/scenarios/*.txt; do
        basename "$f" .txt
    done
}

# ── Main ──
main() {
    log "Benchmark harness starting"
    log "Scenario: $SCENARIO | Reps: $REPS | Cooldown: ${COOLDOWN}s"

    # Handle 'all' scenario
    local scenarios=()
    if [[ "$SCENARIO" == "all" ]]; then
        for f in "$SCRIPT_DIR"/scenarios/*.txt; do
            scenarios+=("$(basename "$f" .txt)")
        done
    elif [[ "$SCENARIO" == "list" ]]; then
        list_scenarios
        exit 0
    else
        scenarios+=("$SCENARIO")
    fi

    # Check prerequisites
    if [[ ! -x "$PROXY_BIN" ]]; then
        log "Building proxy binary..."
        (cd "$SCRIPT_DIR/proxy" && go build -o proxy .) || die "Failed to build proxy"
    fi

    command -v claude >/dev/null 2>&1 || die "claude CLI not found in PATH"
    command -v git >/dev/null 2>&1 || die "git not found in PATH"
    command -v python3 >/dev/null 2>&1 || die "python3 not found in PATH"

    if [[ ! -x "$GENESIS_BIN" ]]; then
        die "Genesis binary not found: $GENESIS_BIN"
    fi
    if [[ ! -x "$GENESIS_CLI" ]]; then
        die "Genesis CLI not found: $GENESIS_CLI"
    fi

    # Create run directory
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    local run_base="${BENCH_DIR}/runs/${timestamp}"
    mkdir -p "$run_base"

    # Symlink latest
    ln -sfn "$run_base" "${BENCH_DIR}/runs/latest"

    # Save run metadata
    cat > "$run_base/meta.json" <<EOF
{
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "scenarios": $(printf '%s\n' "${scenarios[@]}" | jq -R . | jq -s .),
    "reps": $REPS,
    "cooldown": $COOLDOWN,
    "model": "$MODEL",
    "proxy_port": $PROXY_PORT,
    "heaven_port": $HEAVEN_PORT,
    "repo_url": "$REPO_URL"
}
EOF

    for scenario in "${scenarios[@]}"; do
        log "━━━ Scenario: $scenario ━━━"
        local prompt
        prompt=$(load_prompt "$scenario")

        for rep in $(seq 1 "$REPS"); do
            log "─── Rep $rep/$REPS ───"
            local run_id="${scenario}-R${rep}"

            # Claude run
            local claude_dir="${run_base}/${scenario}/rep${rep}/claude"
            mkdir -p "$claude_dir"
            run_claude "$claude_dir" "$prompt" "claude-${run_id}"

            # Cooldown between runs
            if [[ $COOLDOWN -gt 0 ]]; then
                log "Cooling down for ${COOLDOWN}s..."
                sleep "$COOLDOWN"
            fi

            # Genesis run
            local genesis_dir="${run_base}/${scenario}/rep${rep}/genesis"
            mkdir -p "$genesis_dir"
            run_genesis "$genesis_dir" "$prompt" "genesis-${run_id}"

            # Cooldown between reps (skip after last)
            if [[ $rep -lt $REPS ]] && [[ $COOLDOWN -gt 0 ]]; then
                log "Cooling down for ${COOLDOWN}s..."
                sleep "$COOLDOWN"
            fi
        done
    done

    log "━━━ All runs complete ━━━"
    log "Results in: $run_base"

    # Run analysis if Python deps are available
    if python3 -c "import json, os" 2>/dev/null; then
        log "Running analysis..."
        python3 "${SCRIPT_DIR}/analyze.py" "$run_base" || log "Analysis failed (non-fatal)"
        if [[ -f "$run_base/analysis.json" ]]; then
            python3 "${SCRIPT_DIR}/report.py" "$run_base/analysis.json" || log "Report generation failed (non-fatal)"
        fi
    else
        log "Skipping analysis (Python not available or missing deps)"
    fi

    log "Done. Output: $run_base"
}

main "$@"
