# Genesis SSMP Architecture

### A Pitch for Apple

---

## Executive Summary

Genesis is a **local-first agent orchestrator** that achieves **11.7x to 77x token savings** over conventional approaches by externalizing agent memory into a content-addressed shared state, using structured Edit IR output instead of full-file dumps, and enforcing deterministic page-fault protocols for context loading.

Where existing agent frameworks burn tokens re-reading entire files and holding massive context windows, Genesis introduces a fundamentally different model: agents are **stateless**; all knowledge lives in a shared memory plane called **Heaven**. Agents (called **Angels**) request only the context they need, when they need it, through a structured **Page Fault** protocol.

The result: the same coding tasks completed with a fraction of the tokens, deterministic execution guarantees, and a coordination model that scales from single-agent to multi-agent swarms.

---

## System Overview

Genesis is built on a three-layer architecture inspired by operating system design:

```
┌─────────────────────────────────────────────────────────────────┐
│                         CLI / USER LAYER                        │
│          TUI Interface  ·  Slash Commands  ·  MCP Server        │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│                       GOD (Orchestrator)                         │
│                                                                  │
│  ┌──────────┐  ┌────────────┐  ┌──────────┐  ┌──────────────┐  │
│  │ Planner  │→ │  Prompt    │→ │ Provider │→ │  Integrator  │  │
│  │          │  │  Compiler  │  │ (Angel)  │  │              │  │
│  └──────────┘  └────────────┘  └──────────┘  └──────────────┘  │
│       │                              │               │          │
│  ┌────▼─────┐  ┌────────────┐  ┌────▼─────┐  ┌─────▼────────┐ │
│  │ Mission  │  │   Meter    │  │  Oracle  │  │   Verifier   │ │
│  │   DAG    │  │            │  │          │  │              │ │
│  └──────────┘  └────────────┘  └──────────┘  └──────────────┘ │
│                                                                  │
│   ISA Compiler  ·  AA Compiler  ·  Macro Ops  ·  Output VM      │
│   Solo Mode  ·  Recorder  ·  Clock                              │
└──────────────────────────────┬──────────────────────────────────┘
                               │  Page Faults / REST API
┌──────────────────────────────▼──────────────────────────────────┐
│                  HEAVEN (Shared-State Memory Plane)              │
│                                                                  │
│  ┌───────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │
│  │ BlobStore │  │ EventLog │  │ IR Index │  │    Leases    │  │
│  │ (SHA-256) │  │ (JSONL)  │  │ (SQLite) │  │  (Exclusive) │  │
│  └───────────┘  └──────────┘  └──────────┘  └──────────────┘  │
│                                                                  │
│  ┌───────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │
│  │ FileClock │  │ PFRouter │  │  Prompt  │  │  Sectioner   │  │
│  │ (Monoton) │  │          │  │  Store   │  │  (5-Layer)   │  │
│  └───────────┘  └──────────┘  └──────────┘  └──────────────┘  │
│                                                                  │
│                    HTTP Server (127.0.0.1:4444)                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## The Three Layers

### 1. Heaven — The Shared-State Memory Plane

Heaven is a local HTTP server that provides the persistent, content-addressed memory substrate all agents read from and write to. It is the **single source of truth**.

```
                         ┌──────────────────────────────┐
                         │       Heaven Server           │
                         │     127.0.0.1:4444            │
                         ├──────────────────────────────┤
                         │                              │
          /blob ────────►│  BlobStore                   │
                         │  SHA-256 content-addressed   │
                         │  Immutable, deduped on write │
                         │                              │
         /event ────────►│  EventLog                    │
                         │  Append-only JSONL           │
                         │  Byte-offset cursors         │
                         │                              │
     /ir/build ─────────►│  IR Index                    │
     /ir/symdef          │  SQLite + tree-sitter        │
     /ir/callers         │  Go · Python · TypeScript    │
     /ir/search          │  Symbols, refs, fingerprints │
                         │                              │
     /lease/* ──────────►│  Lease Manager               │
                         │  Exclusive scope locks       │
                         │  Prevents edit conflicts     │
                         │                              │
/file-clock/* ──────────►│  File Clock                  │
                         │  Monotonic per-file counters │
                         │  Stale-edit detection        │
                         │                              │
           /pf ─────────►│  Page Fault Router           │
                         │  On-demand context loading   │
                         │  9 PF commands               │
                         │                              │
     /prompt/* ─────────►│  Prompt Store + Sectioner    │
                         │  Content-addressed prompts   │
                         │  5-layer section algorithm   │
                         └──────────────────────────────┘
```

**Key design principles:**

- **Content-addressed**: Every blob stored at `sha256/<hash>`. Write the same content twice, stored once.
- **Append-only events**: All state changes logged. Full replay possible. Leases and clocks rebuild from event history on boot.
- **Immutable blobs**: Once written, never modified. Agents can always trust blob contents match their hash.

#### IR Index — The Codebase Brain

The IR Index uses SQLite3 with tree-sitter parsers to build a searchable index of every symbol in the repository:

```
┌─────────────────────────────────────────────┐
│                   IR Index                   │
├─────────────────────────────────────────────┤
│                                             │
│  Source Files                               │
│  ┌─────────┐  tree-sitter   ┌───────────┐  │
│  │ *.go    │───────────────►│  symbols   │  │
│  │ *.py    │                │  ┌───────┐ │  │
│  │ *.ts    │                │  │ name  │ │  │
│  └─────────┘                │  │ kind  │ │  │
│       │                     │  │ file  │ │  │
│       │                     │  │ line  │ │  │
│       │                     │  └───────┘ │  │
│       │                     ├───────────┤  │
│       │                     │   refs     │  │
│       └────────────────────►│  ┌───────┐ │  │
│                             │  │ name  │ │  │
│                             │  │ file  │ │  │
│                             │  │ line  │ │  │
│                             │  └───────┘ │  │
│                             ├───────────┤  │
│                             │   files    │  │
│                             │  ┌───────┐ │  │
│                             │  │ path  │ │  │
│                             │  │ hash  │ │  │
│                             │  └───────┘ │  │
│                             └───────────┘  │
│                                             │
│  Incremental: only re-indexes changed files │
│  (fingerprint comparison on build)          │
└─────────────────────────────────────────────┘
```

#### Lease Manager — Conflict Prevention

```
  Agent A                    Heaven                    Agent B
    │                          │                          │
    │  ACQUIRE file:main.go    │                          │
    │─────────────────────────►│                          │
    │          ✓ granted       │                          │
    │◄─────────────────────────│                          │
    │                          │                          │
    │                          │  ACQUIRE file:main.go    │
    │                          │◄─────────────────────────│
    │                          │          ✗ denied        │
    │                          │─────────────────────────►│
    │                          │                          │
    │  RELEASE file:main.go    │                          │
    │─────────────────────────►│                          │
    │          ✓ released      │                          │
    │◄─────────────────────────│                          │
    │                          │                          │
    │                          │  ACQUIRE file:main.go    │
    │                          │◄─────────────────────────│
    │                          │          ✓ granted       │
    │                          │─────────────────────────►│
```

Scopes are typed (`file:path`, `symbol:name`). Same-owner reacquire is idempotent.

---

### 2. God — The Orchestrator

God is the brain. It decomposes high-level tasks into structured **Missions**, compiles them with precisely-selected context, dispatches them to LLM **Angels**, and integrates the results back into the codebase.

#### The Core Pipeline

```
User Task: "Add exponentiation operator to calculator"
│
▼
┌────────────────────────────────────────────────────────────────────────┐
│  PLANNER                                                               │
│                                                                        │
│  1. Build IR index (via Heaven)                                        │
│  2. Extract keywords from task                                         │
│  3. Query IR for relevant symbols                                      │
│  4. Group symbols into scope buckets (max 3)                           │
│  5. Create Mission DAG:                                                │
│                                                                        │
│     ┌──────────────┐                                                   │
│     │   Analysis   │ (4K token budget)                                 │
│     │   Mission    │                                                   │
│     └──────┬───────┘                                                   │
│            │ depends_on                                                 │
│     ┌──────┴────────────────┐                                          │
│     ▼                       ▼                                          │
│  ┌──────────────┐  ┌──────────────┐                                    │
│  │   Impl #1    │  │   Impl #2    │ (8K token budgets)                 │
│  │ pkg/eval/    │  │ pkg/parse/   │                                    │
│  └──────────────┘  └──────────────┘                                    │
│                                                                        │
│  6. Acquire leases for each bucket's scopes                            │
│  7. Log mission_created events                                         │
└────────────────────────────────┬───────────────────────────────────────┘
                                 │
                                 ▼ (for each mission in DAG order)
┌────────────────────────────────────────────────────────────────────────┐
│  PROMPT COMPILER                                                       │
│                                                                        │
│  Salience Scoring:                                                     │
│  ┌────────────────────────────────────────────┐                        │
│  │  Shard Type        │  Score Bonus          │                        │
│  ├────────────────────┼───────────────────────┤                        │
│  │  symdef            │  +3.0                 │                        │
│  │  callers           │  +1.5                 │                        │
│  │  test_relevant     │  +2.0                 │                        │
│  │  hotset_hit        │  +1.0                 │                        │
│  │  recently_touched  │  +0.5                 │                        │
│  │  token penalty     │  -0.01 × est_tokens  │                        │
│  └────────────────────────────────────────────┘                        │
│                                                                        │
│  → Deduplicate shards by BlobID                                        │
│  → Greedy pack in descending score order until budget exhausted        │
│  → Output: MissionPack (header + mission JSON + scored shards)         │
└────────────────────────────────┬───────────────────────────────────────┘
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────────────┐
│  PROVIDER (Angel Dispatch)                                             │
│                                                                        │
│  ┌──────────────┐    POST     ┌──────────────┐                        │
│  │ MissionPack  │────────────►│  Cloud LLM   │                        │
│  │              │             │   (Angel)    │                        │
│  │  - header    │◄────────────│              │                        │
│  │  - shards    │  AngelResp  │  May issue   │                        │
│  │  - budget    │             │  PF requests │                        │
│  └──────────────┘             └──────┬───────┘                        │
│                                      │                                 │
│  Adapter wraps with:                 │ Page Faults                     │
│  • Schema validation                 ▼                                 │
│  • 1 retry on violation      ┌──────────────┐                         │
│  • Macro-op auto-expansion   │    Heaven    │                         │
│  • Repair pack (512b cap)    │  PF Router   │                         │
│  • Usage metering            └──────────────┘                         │
└────────────────────────────────┬───────────────────────────────────────┘
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────────────┐
│  INTEGRATOR                                                            │
│                                                                        │
│  7-Step Pipeline:                                                      │
│                                                                        │
│  ① Validate manifest ──► leases + file clocks via Heaven               │
│  ② Detect clock drift ──► attempt simple rebase                        │
│  ③ Snapshot old file contents                                          │
│  ④ Apply Edit IR operations ──► anchor hash verification               │
│  ⑤ Generate diffs for modified/created files                           │
│  ⑥ Increment file clocks via Heaven                                    │
│  ⑦ Log integration event                                               │
│                                                                        │
│  On conflict → generate ConflictMission (AA program, depth-limited)    │
└────────────────────────────────┬───────────────────────────────────────┘
                                 │
                                 ▼
┌────────────────────────────────────────────────────────────────────────┐
│  VERIFIER                                                              │
│                                                                        │
│  1. Detect test command (go test / npm test / pytest / cargo test)      │
│  2. Compute env_hash (lockfiles + tool versions)                       │
│  3. Execute tests, capture stdout                                      │
│  4. Create Receipt → store as blob in Heaven                           │
│  5. GateMerge: exit_code == 0 → allow merge                           │
└────────────────────────────────────────────────────────────────────────┘
```

---

### 3. Edit IR — The Token-Efficient Output Format

Instead of agents outputting entire files or traditional diffs, Genesis uses **Edit IR** — a structured, verifiable instruction set for code modifications.

```
Traditional Agent Output (wasteful)        Genesis Edit IR (efficient)
──────────────────────────────            ─────────────────────────────
"Here's the entire updated file:          {
                                            "ops": [
 package calc                                {
                                               "op": "replace_span",
 func Add(a, b int) int {                     "path": "calc.go",
   return a + b                                "lines": [12, 14],
 }                                             "anchor_hash": "a3f2...",
                                               "content": "func Pow(..."
 func Sub(a, b int) int {                   },
   return a - b                              {
 }                                             "op": "insert_after_symbol",
                                               "path": "calc_test.go",
 func Pow(a, b int) int {   ← new             "symbol": "TestSub",
   result := 1                                 "content": "func TestPow..."
   for i := 0; i < b; i++ {                 }
     result *= a                           ]
   }                                      }
   return result
 }                                        ← Only the changes. Verified by
                                            anchor hash (SHA-256 of 3 lines
 func TestAdd(t *testing.T) {...}           before + 3 lines after).
 func TestSub(t *testing.T) {...}
 func TestPow(t *testing.T) {...} ← new

 ..."

 ← The ENTIRE file re-sent,
   wasting 90%+ of tokens on
   unchanged code.
```

**Edit IR Operations:**

| Operation | Purpose | Fields |
|-----------|---------|--------|
| `replace_span` | Replace lines in a file | path, lines, anchor_hash, content |
| `delete_span` | Delete lines from a file | path, lines, anchor_hash |
| `insert_after_symbol` | Insert code after a symbol | path, symbol, content |
| `add_file` | Create a new file | path, content |

**Anchor Hash Verification:**

```
File: calc.go                      Anchor = SHA-256 of:
──────────────                     lines [9,10,11] + lines [15,16,17]
 9 │                               (3 before span + 3 after span)
10 │ func Sub(a, b int) int {
11 │   return a - b                If another agent modified these
12 │ }  ◄── span start            surrounding lines, the hash won't
13 │                               match → conflict detected →
14 │ // end of math ops            automatic rebase or escalation
15 │ ◄── span end
16 │ func TestAdd(...) {
17 │   ...
```

---

## Page Fault Protocol

The Page Fault (PF) protocol is Genesis's on-demand context loading system, inspired by OS virtual memory. Instead of stuffing the entire codebase into a prompt, agents **fault in** only the context they need.

```
┌──────────────┐                    ┌──────────────┐
│              │                    │              │
│    Angel     │   PF_SYMDEF foo   │    Heaven    │
│   (Agent)    │───────────────────►│   PFRouter   │
│              │                    │              │
│              │   shards: [       │              │
│              │◄──────────────────│  IR Index    │
│              │     {kind:symdef, │  BlobStore   │
│              │      blob_id:..., │              │
│              │      meta:{...}}  │              │
│              │   ]               │              │
│              │                    │              │
│              │   PF_CALLERS foo  │              │
│              │───────────────────►│              │
│              │                    │              │
│              │   shards: [...]   │              │
│              │◄──────────────────│              │
│              │                    │              │
└──────────────┘                    └──────────────┘
```

**Available PF Commands:**

| Command | Purpose | Returns |
|---------|---------|---------|
| `PF_STATUS` | Heaven state revision | State rev, lease count, clock snapshots |
| `PF_SYMDEF <symbol>` | Symbol definition + context | Definition source + related symdefs |
| `PF_CALLERS <symbol> [top_k]` | Who calls this symbol? | Caller references (default top 10) |
| `PF_SLICE <path> <start> <n>` | Read N lines from a file | Raw file content slice |
| `PF_SEARCH <query> [top_k]` | Semantic code search | Matching symbols and references |
| `PF_TESTS <symbol>` | Test files for a symbol | Related test functions |
| `PF_PROMPT_SECTION <id> <idx>` | Single prompt section | Section content |
| `PF_PROMPT_SEARCH <id> <query>` | Search within prompt | Matching sections |
| `PF_PROMPT_SUMMARY <id>` | Prompt table of contents | Section titles + types + sizes |

**Budgeted**: Solo mode enforces a max PF call limit (default 10). Budget remaining is reported in every PF response, preventing context storms.

---

## Execution Modes

Genesis supports two execution modes optimized for different task sizes:

### Swarm Mode (Default — Multi-Agent)

```
                    ┌──────────────────┐
                    │   User Task      │
                    └────────┬─────────┘
                             │
                    ┌────────▼─────────┐
                    │     Planner      │
                    │   (DAG Builder)  │
                    └────────┬─────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
      ┌──────────────┐ ┌──────────┐ ┌──────────────┐
      │  Analysis    │ │  Impl A  │ │   Impl B     │
      │  (4K tokens) │ │ (8K tok) │ │  (8K tok)    │
      └──────┬───────┘ └────┬─────┘ └──────┬───────┘
             │              │              │
             │    ┌─────────┴──────────┐   │
             │    ▼                    ▼   │
             │  ┌──────┐          ┌──────┐ │
             │  │Angel │          │Angel │ │
             │  │  #1  │          │  #2  │ │
             │  └──┬───┘          └──┬───┘ │
             │     │                 │     │
             │     ▼                 ▼     │
             │  ┌──────────────────────┐   │
             │  │     Integrator       │   │
             │  │  (parallel apply)    │   │
             │  └──────────┬───────────┘   │
             │             │               │
             │             ▼               │
             │  ┌──────────────────────┐   │
             │  │      Verifier        │   │
             │  │   (test + receipt)   │   │
             │  └──────────────────────┘   │
             │                             │
             └─────────────────────────────┘

  Multiple agents work in parallel on separate scopes.
  Leases prevent conflicts. DAG enforces ordering.
  Benchmarked: 11.7x — 18.3x token savings.
```

### Solo Mode (Single Agent with Paging)

```
┌──────────────────────────────────────────────────┐
│                  Solo Mission                     │
│                                                  │
│  Phase Loop:                                     │
│                                                  │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐    │
│  │UNDERSTAND│──►│   PLAN   │──►│ EXECUTE  │    │
│  │          │   │          │   │          │    │
│  │ PF calls │   │ PF calls │   │ Edit IR  │    │
│  │ to read  │   │ to scope │   │ output   │    │
│  │ context  │   │ changes  │   │          │    │
│  └──────────┘   └──────────┘   └────┬─────┘    │
│                                      │          │
│                                      ▼          │
│                                ┌──────────┐    │
│                                │  VERIFY  │    │
│                                │          │    │
│                                │ Run tests│    │
│                                └──────────┘    │
│                                                  │
│  Working set: recently touched symbols cached    │
│  across PF calls. Max 10 PFs, 3 turns.          │
│                                                  │
│  Benchmarked: 33.5x — 77.0x token savings.      │
└──────────────────────────────────────────────────┘
```

---

## Instruction Set Architectures

Genesis includes two DSLs for deterministic task specification, allowing tasks to be compiled rather than interpreted.

### Angel Assembly (AA) — The Task DSL

```
# Example: Add pow operation to calculator
BASE_REV abc123
LEASE symbol:Eval symbol:Parser file:calc.go
NEED symdef Eval
NEED callers Eval
NEED slice calc.go 1 50
DO Implement a Pow(base, exp int) function in calc.go
DO Add test coverage for Pow in calc_test.go
ASSERT tests calc
RETURN edit_ir
```

AA compiles to: `Mission` + `ShardRequest[]`

### Genesis ISA — The Full Control Language

```
VERSION 0
BASE_REV abc123
PROMPT_REF sha256:def456
MODE SOLO
BUDGET 12000
INVARIANT No changes to public API signatures
INVARIANT All functions must have test coverage
NEED symdef Eval
NEED callers Eval
OP Implement Pow function following existing patterns
OP Add comprehensive test cases
RUN tests calc
ASSERT exit_code == 0
IF_FAIL RETRY 2
IF_FAIL ESCALATE
HALT
```

**Compilation chain:**

```
ISA Program ──► ISA Compiler ──► AA Program ──► AA Compiler ──► Mission + ShardRequests
                (lower())                       (compile())
```

---

## Prompt VM — Long-Prompt Optimization

For complex tasks with large specifications, the Prompt VM stores prompts as artifacts and pages sections on demand:

```
┌────────────────────────────────────────────────────────────┐
│  User Prompt (12KB specification)                          │
│                                                            │
│  "Build a REST API for user management with OAuth2..."     │
│  [... 12KB of requirements, examples, constraints ...]     │
└────────────────────────────────┬───────────────────────────┘
                                 │
                    ┌────────────▼───────────────┐
                    │   5-Layer Sectioner         │
                    │                             │
                    │   L0: Preserve raw bytes    │
                    │   L1: Code fences kept whole│
                    │   L2: Split on headings     │
                    │   L3: Classify by type      │
                    │   L4: Chunk large sections  │
                    └────────────┬───────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
  ┌────────────────┐ ┌────────────────┐ ┌────────────────┐
  │  constraints   │ │  acceptance    │ │     api        │
  │  score: 3.0    │ │  score: 2.5    │ │  score: 2.0    │
  │  PINNED for    │ │  PINNED for    │ │  PINNED for    │
  │  all roles     │ │  reviewer      │ │  builder       │
  └────────────────┘ └────────────────┘ └────────────────┘
              ▼                  ▼                  ▼
  ┌────────────────┐ ┌────────────────┐ ┌────────────────┐
  │   security     │ │   examples     │ │    style       │
  │  score: 2.0    │ │  score: 1.0    │ │  score: 1.5    │
  │  PAGED         │ │  PAGED         │ │  PAGED         │
  │  via PF_PROMPT │ │  via PF_PROMPT │ │  via PF_PROMPT │
  └────────────────┘ └────────────────┘ └────────────────┘

  Only high-scoring sections inlined. Rest available via PF.
  Role-based pinning: planner, builder, reviewer get different sections.
  Result: 5x-8x savings on multi-turn workflows.
```

**Section type scoring:**

| Section Type | Score | Typically Pinned For |
|-------------|-------|---------------------|
| constraints | 3.0 | All roles |
| acceptance | 2.5 | Reviewer |
| api | 2.0 | Builder |
| security | 2.0 | All roles |
| style | 1.5 | Builder |
| spec | 1.0 | Planner |
| examples | 1.0 | On demand (PF) |
| glossary | 0.5 | On demand (PF) |

---

## Oracle — Escalation & Replanning

When an agent gets stuck (thrashing), the Oracle steps in:

```
  Mission executing...
      │
      │  Meter detects thrashing:
      │  • >20 PF calls in 2 consecutive turns
      │  • >2 schema rejects
      │  • >3 integration conflicts
      │  • >3 test failures
      │
      ▼
  ┌──────────────┐
  │   Oracle     │
  │              │
  │  Sends to    │
  │  cloud LLM:  │
  │              │
  │  • spec_blob │      ┌──────────────┐
  │  • decision  │─────►│   Cloud LLM  │
  │    ledger    │      │              │
  │  • failing   │◄─────│  Returns:    │
  │    tests     │      │  • new DAG   │
  │  • symbol    │      │  • new leases│
  │    shortlist │      │  • risk spots│
  │  • metrics   │      │  • rec tests │
  │              │      └──────────────┘
  │  (16K budget)│
  └──────┬───────┘
         │
         ▼
  Validates new DAG for cycles
  Replaces stuck mission plan
  Execution continues
```

---

## Conflict Detection & Resolution

```
  Agent A writes Edit IR          Agent B writes Edit IR
  for calc.go lines 12-14        for calc.go lines 10-16
         │                                 │
         ▼                                 ▼
  ┌──────────────┐               ┌──────────────┐
  │  Integrator  │               │  Integrator  │
  │              │               │              │
  │ ① Check     │               │ ① Check     │ ← Lease denied!
  │   lease ✓   │               │   lease ✗   │   (Agent A holds it)
  │              │               │              │
  │ ② Check     │               │   BLOCKED    │
  │   file      │               └──────────────┘
  │   clock ✓   │
  │              │
  │ ③ Verify    │   If anchor hash mismatch:
  │   anchor    │──────────────────────────────┐
  │   hash      │                              │
  │              │                              ▼
  │ ④ Apply ✓   │                    ┌──────────────────┐
  │              │                    │ Attempt Rebase   │
  │ ⑤ Increment │                    │ (recompute       │
  │   clock     │                    │  anchor hashes)  │
  └──────────────┘                    └────────┬─────────┘
                                               │
                                    ┌──────────┴──────────┐
                                    ▼                     ▼
                              Rebase OK              Rebase Fails
                              (apply)                     │
                                                          ▼
                                               ┌──────────────────┐
                                               │ Conflict Mission │
                                               │ (AA program with │
                                               │  depth limit = 2 │
                                               │  to prevent      │
                                               │  infinite loops) │
                                               └──────────────────┘
```

---

## Metering & Observability

Every mission is tracked with fine-grained metrics:

```
┌─────────────────────────────────────────────┐
│              Mission Meter                   │
├─────────────────────────────────────────────┤
│                                             │
│  pf_count              12                   │
│  pf_response_size      34,521 bytes         │
│  retries               1                    │
│  rejects               0                    │
│  conflicts             0                    │
│  test_failures         0                    │
│  tokens_in             4,200                │
│  tokens_out            1,800                │
│  turns                 2                    │
│  elapsed_ms            3,450                │
│  status                completed            │
│                                             │
│  Per-turn PF tracking:                      │
│  turn 1: 5 PFs                              │
│  turn 2: 7 PFs                              │
│                                             │
│  Thrash detection:                          │
│  >20 PFs × 2 consecutive turns → escalate  │
│                                             │
└─────────────────────────────────────────────┘
```

When `GENESIS_LEAN=1` is set, metrics encode in TSLN (Tab-Separated Lean Notation) for compact storage and transmission.

---

## Record & Replay

Genesis supports deterministic execution recording for testing and debugging:

```
┌──────────────┐        ┌──────────────────┐        ┌──────────────┐
│   Provider   │───────►│ RecordingProvider │───────►│  Cloud LLM   │
│              │        │                  │        │              │
│              │        │  Logs to JSONL:  │        │              │
│              │        │  • pack hash     │◄───────│              │
│              │        │  • response      │        │              │
│              │        │  • timestamp     │        │              │
└──────────────┘        └──────────────────┘        └──────────────┘

                               │
                     ┌─────────▼─────────┐
                     │  recording.jsonl   │
                     │                    │
                     │  {hash: "a1b2",    │
                     │   resp: "..."}     │
                     │  {hash: "c3d4",    │
                     │   resp: "..."}     │
                     └─────────┬─────────┘
                               │
┌──────────────┐        ┌──────▼───────────┐
│   Provider   │───────►│  ReplayProvider  │  No network calls.
│              │        │                  │  Matches by hash
│              │◄───────│  (deterministic) │  or mission ID.
│              │        │                  │
└──────────────┘        └──────────────────┘
```

---

## Authentication & Provider Support

Genesis is provider-agnostic with multi-provider authentication:

```
┌──────────────────────────────────────────────────┐
│              Provider Backends                    │
├──────────────────────────────────────────────────┤
│                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────┐ │
│  │  Anthropic  │  │   OpenAI    │  │  Gemini │ │
│  │  (Claude)   │  │  (GPT-4+)  │  │         │ │
│  └─────────────┘  └─────────────┘  └─────────┘ │
│                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────┐ │
│  │ OpenRouter  │  │    Groq     │  │   xAI   │ │
│  │             │  │             │  │         │ │
│  └─────────────┘  └─────────────┘  └─────────┘ │
│                                                  │
│  ┌─────────────────────────────────────────────┐ │
│  │           GitHub Copilot                    │ │
│  │     (Device Flow + gh CLI bridge)           │ │
│  └─────────────────────────────────────────────┘ │
│                                                  │
│  Credential Storage:                             │
│  • OS keyring (preferred)                        │
│  • ~/.config/genesis/credentials/ (0600)         │
│                                                  │
│  Soft Enforcement:                               │
│  Warns if full prompt leaks into pack            │
│  (detects PromptRef.TotalTokens × 4 < bytes)    │
└──────────────────────────────────────────────────┘
```

---

## MCP Connector — Remote Integration

Genesis exposes a Model Context Protocol server for external tool integration:

```
┌──────────────┐    SSE + JSON-RPC    ┌──────────────────────┐
│   External   │◄────────────────────►│   MCP Server         │
│   Tool       │                      │   127.0.0.1:5555     │
│              │                      │                      │
│   (IDE,      │   Bearer Token Auth  │   10 Tools:          │
│    CI/CD,    │   120 req/min limit  │   • ping             │
│    Script)   │                      │   • status           │
│              │                      │   • events           │
└──────────────┘                      │   • ir_search        │
                                      │   • ir_symdef        │
                                      │   • ir_callers       │
                                      │   • ir_build         │
                                      │   • lease_acquire    │
                                      │   • validate_manifest│
                                      │   • blob_store       │
                                      │                      │
                                      │   No file system     │
                                      │   tools exposed.     │
                                      └──────────────────────┘
```

---

## Benchmark Results

### Token Savings — Swarm Mode

```
                Baseline (3 agents, 2 turns)     Genesis        Ratio
              ─────────────────────────────   ───────────   ──────────
  Task A          17,000 input tokens           1,500          11.7x
  Task B          18,000 input tokens           1,100          16.9x
  Task C          22,000 input tokens           1,200          18.3x
              ─────────────────────────────   ───────────   ──────────
  Target: 5x                                              ✓ All exceed
```

### Token Savings — Solo Mode

```
                Baseline                        Genesis Solo    Ratio
              ─────────────────────────────   ───────────   ──────────
  Simple          5,000 input tokens              150          33.5x
  Complex        15,000 input tokens              195          77.0x
              ─────────────────────────────   ───────────   ──────────
  Target: 10x                                             ✓ All exceed
```

### Why Genesis Wins

```
┌──────────────────────────────────────────────────────────────────┐
│                                                                  │
│  Conventional Agent                    Genesis Agent             │
│  ─────────────────                    ──────────────             │
│                                                                  │
│  Turn 1:                              Turn 1:                    │
│  ┌────────────────────┐               ┌────────────────────┐    │
│  │ System prompt 2K   │               │ Header 200         │    │
│  │ Full file A  3K    │               │ Mission JSON 150   │    │
│  │ Full file B  4K    │               │ Shard: symdef 300  │    │
│  │ Full file C  2K    │               │ Shard: callers 200 │    │
│  │ Instructions 1K    │               │ PF playbook 100    │    │
│  ├────────────────────┤               ├────────────────────┤    │
│  │ Total: 12K tokens  │               │ Total: 950 tokens  │    │
│  └────────────────────┘               └────────────────────┘    │
│                                                                  │
│  Turn 2:                              Turn 2:                    │
│  ┌────────────────────┐               ┌────────────────────┐    │
│  │ System prompt 2K   │ ← resent     │ PF_SYMDEF Foo 250  │    │
│  │ Full file A  3K    │ ← resent     │ Edit IR response    │    │
│  │ Full file B  4K    │ ← resent     │   150 tokens       │    │
│  │ Full file C  2K    │ ← resent     ├────────────────────┤    │
│  │ Prior output 2K    │ ← resent     │ Total: 400 tokens  │    │
│  │ New instruct 1K    │               └────────────────────┘    │
│  ├────────────────────┤                                         │
│  │ Total: 14K tokens  │                                         │
│  └────────────────────┘               Cumulative: 1,350         │
│                                       vs 26,000 = 19.3x        │
│  Cumulative: 26K tokens                                         │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

---

## Technology Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.24 |
| Database | SQLite3 (IR Index) |
| Parsing | tree-sitter (Go, Python, TypeScript) |
| Storage | Content-addressed blobs (SHA-256) |
| Event Log | Append-only JSONL |
| HTTP Server | Go `net/http` (Heaven) |
| Contracts | JSON Schema (8 schemas in proto/) |
| Testing | 213 tests (contract, concurrency, determinism, adversarial, benchmark) |
| CLI | 14 slash commands, TUI interface |

---

## Codebase Size

| Layer | Files | Description |
|-------|-------|-------------|
| **god/** | ~37 | Orchestrator: planner, compiler, provider, integrator, verifier, oracle, ISA/AA compilers, solo mode, recorder, meters |
| **heaven/** | ~20 | Memory plane: blob store, event log, IR index, leases, file clocks, PF router, prompt store, sectioner, HTTP server |
| **cli/** | ~90 | User interface: TUI, slash commands, auth, dialogs |
| **proto/** | 8 | JSON Schema contracts |
| **internal/** | ~10 | Test kit, shared utilities |
| **bench/** | 4 | Benchmark harness, baseline, genesis, assertions |
| **docs/** | 5 | AUTH, HARDMODE, MCP_CONNECTOR, PROMPT_VM, SOLO_MODE |
| **fixtures/** | ~10 | Test repositories, sample ISA, sample prompts |

---

## Data Flow — Complete End-to-End

```
                              ┌─────────┐
                              │  USER   │
                              │  TASK   │
                              └────┬────┘
                                   │
                   ┌───────────────▼───────────────┐
                   │           CLI / TUI            │
                   └───────────────┬───────────────┘
                                   │
                   ┌───────────────▼───────────────┐
                   │          PLANNER               │
                   │                                │
                   │  Keywords → IR Query → Buckets │
                   │  → Mission DAG + Leases        │
                   └───────────────┬───────────────┘
                                   │
                    ┌──────────────┴──────────────┐
                    ▼                             ▼
           ┌───────────────┐            ┌───────────────┐
           │    PROMPT      │            │    PROMPT      │
           │   COMPILER     │            │   COMPILER     │
           │                │            │                │
           │ Score shards   │            │ Score shards   │
           │ Pack to budget │            │ Pack to budget │
           └───────┬───────┘            └───────┬───────┘
                   │                             │
                   ▼                             ▼
           ┌───────────────┐            ┌───────────────┐
           │   PROVIDER     │            │   PROVIDER     │
           │   (Angel #1)   │            │   (Angel #2)   │
           │                │            │                │
           │   ┌─── PF ───┐│            │   ┌─── PF ───┐│
           │   │  HEAVEN   ││            │   │  HEAVEN   ││
           │   └───────────┘│            │   └───────────┘│
           │                │            │                │
           │  → Edit IR     │            │  → Edit IR     │
           └───────┬───────┘            └───────┬───────┘
                   │                             │
                   ▼                             ▼
           ┌───────────────┐            ┌───────────────┐
           │  INTEGRATOR    │            │  INTEGRATOR    │
           │                │            │                │
           │  Validate      │            │  Validate      │
           │  Apply         │            │  Apply         │
           │  Diff          │            │  Diff          │
           └───────┬───────┘            └───────┬───────┘
                   │                             │
                   └──────────────┬──────────────┘
                                  │
                   ┌──────────────▼──────────────┐
                   │          VERIFIER            │
                   │                              │
                   │    Run tests → Receipt       │
                   │    GateMerge → Allow/Deny    │
                   └──────────────┬──────────────┘
                                  │
                   ┌──────────────▼──────────────┐
                   │          METER               │
                   │                              │
                   │   Track: tokens, PFs, turns  │
                   │   Detect thrashing           │
                   │   → Oracle escalation        │
                   └──────────────────────────────┘
```

---

## Key Innovations Summary

| Innovation | What It Does | Impact |
|-----------|-------------|--------|
| **Edit IR** | Structured code modifications with anchor hash verification | Eliminates full-file token waste |
| **Heaven SSMP** | Content-addressed shared state with immutable blobs | Agents are stateless; full dedup |
| **Salience Packing** | Score-based context selection under token budget | Only the most relevant context sent |
| **Page Fault Protocol** | On-demand context loading with budgets | No context storms; lazy loading |
| **Prompt VM** | 5-layer sectioning with role-based pinning | 5x-8x savings on long prompts |
| **Lease Coordination** | Exclusive scope locks with file clocks | Zero edit conflicts at integration |
| **Oracle Escalation** | Automatic thrash detection and replanning | Self-healing execution |
| **ISA/AA Compilers** | Deterministic task specification DSLs | Reproducible, testable missions |
| **Record/Replay** | Hash-based deterministic replay | Test without LLM calls |
| **Anchor Hashing** | SHA-256 of surrounding context | Bit-perfect conflict detection |

---

*Genesis SSMP — Built in Go. Local-first. Provider-agnostic. 11.7x to 77x more efficient.*
