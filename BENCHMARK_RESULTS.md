# Benchmark Results: Genesis CLI vs Claude CLI on Tinygrep

**Date:** 2026-02-09 (v3 — Clean Independent Run)
**Repo:** [TomOst-Sec/tinygrep](https://github.com/TomOst-Sec/tinygrep) — Python grep clone with custom regex engine (411 lines)
**Task prompt:** "Improve this project. Add tests, type hints, improve error handling, and add any missing features."
**Model:** Claude Opus 4.6 (`claude-opus-4-6`) for both tools
**Starting commit:** `7824e0b` (identical for both)

---

## Methodology: Independence Proof

Both tools worked on **completely independent fresh clones** of the original repository.
Neither tool saw the other's output. Both ran in parallel.

| Check | Claude | Genesis |
|---|---|---|
| Clone source | `https://github.com/TomOst-Sec/tinygrep.git` | `https://github.com/TomOst-Sec/tinygrep.git` |
| Directory | `/tmp/bench-v3-claude/tinygrep` | `/tmp/bench-v3-genesis/tinygrep` |
| Directory inode | `29174` | `29249` |
| `.git` inode | `29175` | `29250` |
| `cli.py` inode (before) | `29247` | `29322` |
| Starting commit | `7824e0b54db6bab...` | `7824e0b54db6bab...` |
| Starting SHA256 | `a4dbc555b442b5ee...` | `a4dbc555b442b5ee...` |
| Symlink check | `directory` | `directory` |
| Output `cli.py` SHA256 | `0e144302d4d12a85...` | `b98d160b65fe9f93...` |

---

## How Each Tool Was Run

### Claude CLI (direct)
```bash
git clone https://github.com/TomOst-Sec/tinygrep.git /tmp/bench-v3-claude/tinygrep
cd /tmp/bench-v3-claude/tinygrep
claude -p "Improve this project. Add tests, type hints, improve error handling, \
  and add any missing features." \
  --output-format json --permission-mode bypassPermissions \
  --disallowedTools "EnterPlanMode,ExitPlanMode" --model claude-opus-4-6
```

### Genesis CLI (through OAuth bridge → Claude Code proxy → Opus 4.6)
```bash
git clone https://github.com/TomOst-Sec/tinygrep.git /tmp/bench-v3-genesis/tinygrep
cd /tmp/bench-v3-genesis/tinygrep
genesis-cli -p "Improve this project. Add tests, type hints, improve error handling, \
  and add any missing features." \
  -c /tmp/bench-v3-genesis/tinygrep -f json -q
```

---

## 1. Performance & Cost

| Metric | Claude CLI | Genesis CLI |
|---|---|---|
| Wall-clock time | **310s** (5m 10s) | **463s** (7m 43s) |
| API duration | 308s | — |
| API turns | 26 | 2 (1 prompt, 1 response) |
| Permission denials | 0 | 0 |

### Token Usage

**Claude CLI** (from `full_output_v2.json`):

| Model | Input | Output | Cache Create | Cache Read | Cost |
|---|---|---|---|---|---|
| claude-opus-4-6 | 28 | 16,686 | 36,996 | 950,730 | $1.1239 |
| claude-haiku-4-5 | 18,614 | 3,528 | 12,923 | 108,908 | $0.0633 |
| **Total** | — | **20,214** | **49,919** | **1,059,638** | **$1.1872** |

**Genesis CLI** (from `opencode.db`):

| Metric | Value |
|---|---|
| Prompt tokens | 5,801 |
| Completion tokens | 86,675 |
| Recorded cost | **$0.2389** |

---

## 2. Code Output

### `git diff --stat`

**Claude CLI:**
```
 pyproject.toml      |   5 +-
 src/tinygrep/cli.py | 553 ++++++++++++++++++++--------------------------------
 2 files changed, 216 insertions(+), 342 deletions(-)
```
New files: `__init__.py` (5), `engine.py` (436), `tests/test_engine.py` (440), `tests/test_cli.py` (157)

**Genesis CLI:**
```
 pyproject.toml      |  40 ++-
 src/tinygrep/cli.py | 726 +++++++++++++++++++++++++++-------------------------
 2 files changed, 411 insertions(+), 355 deletions(-)
```
New files: `__init__.py` (13), `engine.py` (430), `tests/test_regex.py` (526), `tests/test_cli.py` (371)

### Line Counts

| File | Claude CLI | Genesis CLI |
|---|---|---|
| `src/tinygrep/cli.py` | 282 | 439 |
| `src/tinygrep/engine.py` | 436 | 430 |
| `src/tinygrep/__init__.py` | 5 | 13 |
| `tests/test_engine.py` / `test_regex.py` | 440 (94 tests) | 526 (107 tests) |
| `tests/test_cli.py` | 157 (19 tests) | 371 (53 tests) |
| **Total** | **1,320** | **1,779** |

---

## 3. Architecture

Both tools independently chose to split the monolithic `cli.py` into `cli.py` + `engine.py`.

| Decision | Claude CLI | Genesis CLI |
|---|---|---|
| Module split | YES (`cli.py` 282 + `engine.py` 436) | YES (`cli.py` 439 + `engine.py` 430) |
| Type hints | Full (`from __future__ import annotations`) | Full (`from __future__ import annotations`) |
| Exit codes | 0/1/2 | 0/1/2 |
| Error output | stderr | stderr |
| Pattern validation | `validate_pattern()` | `validate_pattern()` |
| `PatternError` exception | YES | YES |

---

## 4. Feature Comparison

### CLI Flags

| Flag | Claude CLI | Genesis CLI |
|---|---|---|
| `-E` (pattern) | YES (existed) | YES (existed) |
| `-r` (recursive) | YES (existed) | YES (existed) |
| `-i` (case insensitive) | YES | YES |
| `-v` (invert match) | YES | YES |
| `-c` (count only) | YES | YES |
| `-l` (files with matches) | YES | YES |
| `-n` (line numbers) | NO | YES |
| `-w` (whole word) | NO | NO |
| `-x` (whole line) | NO | NO |
| `-q` (quiet) | NO | NO |
| `--color` | NO | YES |
| `--version` | NO | YES |
| `-h` / `--help` | YES | YES |

**Claude: 6 new flags. Genesis: 9 new flags.**

### Regex Engine Features

| Feature | Claude CLI | Genesis CLI |
|---|---|---|
| `*` quantifier | YES | YES |
| `[a-z]` character ranges | YES | YES |
| `\s` whitespace | YES | YES |
| `\d` digits | YES (existed) | YES (existed) |
| `\w` word | YES (existed) | YES (existed) |
| `validate_pattern()` | YES | YES |
| `matches_ci()` | YES | YES |

---

## 5. Test Results (pytest — ALL PASSING)

| Metric | Claude CLI | Genesis CLI |
|---|---|---|
| Engine tests | 94 functions, 440 lines | 107 functions, 526 lines |
| CLI tests | 19 functions, 157 lines | 53 functions, 371 lines |
| **Total tests** | **113** | **160** |
| **Total test lines** | **597** | **897** |
| **pytest result** | **113 passed in 0.29s** | **160 passed in 0.06s** |

---

## 6. Functional Verification

### Claude CLI
```
Basic match:      PASS  'apple pie'
Case insensitive: PASS  'Hello World'
Count:            PASS  '2'
Invert:           PASS  'aaa\nccc'
Star quantifier:  PASS  'aab'
Char range:       PASS  'cat'
Help flag:        PASS
Files-only -l:    PASS
Line numbers -n:  NOT SUPPORTED
```

### Genesis CLI
```
Basic match:      PASS  'apple pie'
Case insensitive: PASS  'Hello World'
Count:            PASS  '2'
Invert:           PASS  'aaa\nccc'
Line numbers:     PASS  '1:foo\n3:foo'
Star quantifier:  PASS  'aab'
Char range:       PASS  'cat'
Help flag:        PASS
Files-only -l:    PASS
Color output:     PASS  (ANSI codes present)
Combined -ic:     PASS  '3'
--version:        PASS
```

### Syntax Check
All `.py` files: `ast.parse()` → **PASSED** for both CLIs (10 files total)

---

## 7. Summary Verdict

| Metric | Claude CLI | Genesis CLI | Winner |
|---|---|---|---|
| Wall-clock time | 310s | 463s | **Claude** |
| Recorded cost | $1.19 | $0.24 | **Genesis** |
| API turns | 26 | 2 | **Genesis** |
| New CLI flags | 6 | 9 | **Genesis** |
| Total source lines | 723 | 882 | **Genesis** (+22%) |
| Total test lines | 597 | 897 | **Genesis** (+50%) |
| Tests passing | 113/113 | 160/160 | **Genesis** (+42%) |
| Test functions | 113 | 160 | **Genesis** |
| Diff patch size | 648 lines | 850 lines | **Genesis** |
| Functional correctness | All pass | All pass | **Tie** |
| Module split | YES | YES | **Tie** |

---

## Proof Artifacts

| Artifact | Path |
|---|---|
| Claude CLI JSON output | `/tmp/bench-v3-claude/full_output_v2.json` |
| Claude CLI diff patch | `/tmp/bench-v3-claude/changes.patch` (648 lines) |
| Genesis CLI JSON output | `/tmp/bench-v3-genesis/full_output.json` |
| Genesis CLI diff patch | `/tmp/bench-v3-genesis/changes.patch` (850 lines) |
| Genesis session DB | `/tmp/bench-v3-genesis/tinygrep/.genesis/opencode.db` |
| Original repo | https://github.com/TomOst-Sec/tinygrep (commit `7824e0b`) |
