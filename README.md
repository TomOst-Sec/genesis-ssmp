# Genesis SSMP

> Local-first Agent Swarm OS that externalizes memory into a shared-state plane.
> Deterministic replay. Content-addressed edits. Zero private agent state.

Current AI coding agents hold private state. They can't coordinate, can't replay, and can't verify each other's work. When two agents touch the same file, you get silent conflicts. When an agent crashes, its context is gone. When you ask "why did it change that line?" — there's no audit trail.

Genesis inverts this. **All agent memory is externalized into a content-addressed shared plane called Heaven.** Agents become stateless workers that page-fault into shared memory — exactly like an OS virtual memory system, but for AI agent context. The result: deterministic replay via append-only event logs, transparent coordination via scoped leases, conflict detection via anchor hashes, and token-efficient lazy-loading via the Page Fault protocol.

This is an operating system for agent swarms, not a framework.

## Building

```bash
# Prerequisites: Go 1.24+, C compiler (for tree-sitter cgo bindings)

make build     # compile genesis binary
make test      # run all tests
make bench-s1  # run single benchmark scenario
make bench-full # run all benchmark scenarios
```

## License

MIT
