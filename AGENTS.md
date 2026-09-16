## AGENTS.md

This file provides guidance to agents when working with code in this repository.

# Project

syla is a Go CLI, daemon, TUI, and SDK that runs any AI agent in a supervised, resumable loop.
Full architecture, the agent roster, install steps, and the CLI reference live in `README.md` — read that first, and link to the relevant section from a commit or PR instead of restating it here.

Everything below is a working default for this codebase, not a law.
These are the patterns that fit the problem as currently understood, in priority order — not a checklist to satisfy mechanically.
If the code in front of you clearly wants something else, write that instead, and say in one line why the default didn't fit.
A better idea beats a followed convention every time.

## 1. Before you touch code
Read `README.md` for the current agent roster and CLI surface — do not infer it from memory of similar tools.
Check whether the change belongs in `pkg/loop`, `pkg/agent`, `pkg/workspace`, `pkg/daemon`, `pkg/keepawake`, `pkg/taskfile`, or `pkg/tui` before writing anything — each package owns exactly one concern, and cross-cutting changes are usually a sign the interface between two packages needs adjusting, not that the new code belongs in both.
Prefer extending an existing interface implementation (a new file under `native/`, `acp/`, or `pty/`; a new `Workspace`; a new `StopCondition`) over adding a special case inside `pkg/loop` itself — `pkg/loop` should stay agent-agnostic and workspace-agnostic.

## 2. Function design
One function, one job, named for exactly that job.
If a function needs "and" to describe what it does, split it.
Small functions composed together are preferred over one large function with internal stages — favor pipelines of narrow functions over branching monoliths.
A function that both computes and formats, or both fetches and validates, is a signal to split, not a style choice to leave alone.

## 3. Composing pieces — patterns as defaults, not mandates
Factory: use a constructor function (`New*`, or a registry `Get(name string)`) at the seam where a concrete type is chosen from a name or config value — this is how `pkg/agent`'s registry should pick between `native`, `acp`, and `pty` implementations, and how `pkg/workspace` should pick `git`, `none`, or `file-snapshot`.
Builder: for anything with several optional fields (`RunConfig`, an `Agent` invocation), prefer the functional-options pattern (`With*` functions over a variadic options slice) over a struct literal with many zero-value fields, or over a chained builder type if the options need validation before use.
Both are defaults for object construction specifically — do not reach for a factory or builder where a plain constructor with two or three arguments already reads clearly. Prefer the plainest thing that works over reflexively applying a pattern.

## 4. Interfaces
Accept interfaces, return concrete types.
Define an interface at the package that consumes it, not the package that implements it — `pkg/loop` defines what it needs from an `Agent` and a `Workspace`; `pkg/agent`'s implementations satisfy that shape, they don't export their own.
Keep interfaces small — one or two methods is normal; a wide interface is usually two interfaces that haven't been split yet.

## 5. Errors
Wrap with context using `fmt.Errorf` and `%w`, never discard an error silently.
Use `errors.Is` / `errors.As` for classification (a `RateLimited` error from an adapter, per the plan's §5.6, needs to be distinguishable this way) rather than string-matching an error message.
Sentinel errors (`var Err... = errors.New(...)`) for conditions calling code needs to branch on; ad hoc `fmt.Errorf` for everything else.

## 6. Concurrency and context
Every function that can block (agent I/O, IPC calls, workspace snapshot/diff operations) takes a `context.Context` as its first argument and respects cancellation.
Own a channel's send side in the goroutine that creates it, and close it from that same goroutine — never let a receiver close a channel it doesn't own.
Prefer a single-writer loop for `RunState` mutation (matches the state machine in the plan) over sharing mutable state across goroutines behind a mutex, when the choice is available.

## 7. Comments
Avoid comments unless the code cannot explain itself — a non-obvious workaround, a subtlety in an external CLI's flags, a reason a simpler approach was rejected.
Prefer a better function or variable name over a comment explaining what a poorly named one does.
Exported identifiers still get a doc comment — that's documentation, not the kind of comment this section is discouraging.

## 8. Testing

- Every new component or meaningful change should include tests in the same change. Do not add production code first and leave testing for later.
- Write unit tests for each component added, covering its normal behavior, important edge cases, and error paths. Use table-driven tests when there are more than two meaningfully different input cases.
- Use integration tests when a component's correctness depends on how multiple packages work together. Test `Agent` and `Workspace` implementations against the interface's contract through a shared test suite, not only with bespoke tests per type — this keeps new adapters honest.
- Use end-to-end tests for user-visible flows that cross package boundaries. An E2E test should exercise the real application path through the CLI and verify the resulting behavior rather than testing internal functions directly. Keep E2E tests deterministic by using fakes or controlled test implementations for external agents and services where appropriate.
- When adding a feature, choose the appropriate combination of unit, integration, and E2E tests rather than assuming one level is sufficient. A component can have unit tests while the feature using it is covered by an E2E test.
- Fakes over mocks for `Agent` and `Workspace` in `pkg/loop` tests — a hand-written fake that records calls is usually clearer than a generated mock here.
- Tests should be part of the definition of done for a change. Run the relevant tests during development, then run the full test suite before considering the change complete.

## 9. Formatting and hygiene
`gofmt` and `go vet` clean before anything is proposed as done; run `staticcheck` if it's already part of this repo's tooling.
No package-level mutable state — pass what a function needs as arguments or receiver fields.
Keep `pkg/loop` free of anything that imports a specific agent adapter, a specific workspace kind, or the TUI — it depends on interfaces only, per §4.

## 10. Where to look next
Agent roster, invocation modes, and per-agent config overrides: `README.md` → Agents.
CLI commands and flags: `README.md` → CLI Reference.
Full system design (daemon/IPC, keep-awake mechanism, stop conditions, iteration isolation): the architecture doc linked from `README.md`.
