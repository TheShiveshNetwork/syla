# syla — "see you later, alligator"

A Go CLI + TUI + SDK for running any coding/AI agent in a supervised, resumable loop, so it can work unattended (overnight, or "until N iterations / a condition / a budget") without the run silently dying.

---

## 1. Core concept, restated precisely

Strip away the branding and syla is three things layered on top of each other:

1. **An agent adapter layer** — a common interface (`Agent`) that knows how to start, prompt, read output from, and stop a specific CLI agent (Claude Code, Codex CLI, Gemini CLI, etc.), normalizing wildly different I/O shapes into one contract.
2. **A loop/orchestration engine** — a state machine that repeatedly drives an `Agent` through iterations, persists state to disk after every step, decides when to continue/stop/retry, and survives crashes, network blips, and agent hangs.
3. **A presentation + control surface** — a TUI that shows the loop live, plus two entrypoints (task-file CLI, and a Go SDK) that both sit on top of the same engine.

Everything else (scraping bot example, "vague" task domain, multi-agent roster) falls out of getting these three layers right. This is good news: it means syla itself never needs to know what the agent is "for." It only needs to know how to talk to the agent process and how to judge iteration boundaries.

---

## 1a. Prior art: gnhf, and where syla needs to diverge

Your inspiration, [`gnhf`](https://github.com/kunchenguid/gnhf) ("good night, have fun"), is a TypeScript "ralph"-style orchestrator that's worth studying closely because it's already solved several of the hard problems below — but it's also instructive in where it's *coupled to coding tasks*, which is exactly the coupling you want syla to avoid.

**What gnhf already validates and syla should borrow the shape of:**
- **Agent roster + adapter table.** Seven native adapters (Claude, Codex, Copilot, Pi, Cursor, Rovo Dev, OpenCode) plus a generic `acp:<target>` escape hatch powered by a bundled ACP (Agent Client Protocol) runtime called `acpx` — this is the "write real adapters, but leave a protocol-based door open for anything ACP-compatible" pattern. **You've confirmed syla needs ACP too**, so this isn't optional the way the first draft framed it: build `pkg/agent/acp/` as a first-class adapter kind alongside `native/` and `pty/` from the start (§1a below), not a later stretch goal. ACP gives you Gemini and any other ACP-compliant agent essentially for free once the client is written once, on top of the hand-written adapters for Claude/Codex/etc. you already said you're fine building.
- **Per-agent config overrides.** `agentPathOverride` (point a name at a custom binary/wrapper) and `agentArgsOverride` (pass through extra flags per agent, e.g. model/reasoning-effort selection) live in a YAML config file, not hardcoded — copy this pattern directly.
- **Failure handling policy, stated precisely.** Consecutive-failure abort threshold (default 3), rollback-on-failure vs preserve-on-commit-failure, retryable vs permanent errors, and a distinct **usage-limit wait** path (detect a rate-limit/quota rejection, read the provider's reset time from the response, sleep until just after it, retry automatically) rather than counting a rate limit as a normal failure. This last one is a real, concrete gap in the plan below — add it as §5.6.
- **Shared memory across iterations via a plain file** (`notes.md`) that each iteration's prompt is built from and each iteration appends to — cheap, debuggable, and doesn't require the adapter to support sessions.
- **A structured "stop" signal the agent emits itself** (`--stop-when <condition>`, evaluated against the agent's own structured JSON result each iteration) — same idea as the `STATUS: DONE` sentinel below, gnhf just makes it a first-class flag rather than a raw regex.
- **An exit summary** — elapsed time, iteration/token totals, diff stats, paths to logs, and copy-pasteable review commands, printed once at the end. Cheap to build, high value for "I woke up, what happened."

**Where gnhf is coupled to git/coding and syla must not be:**
- gnhf's entire execution model assumes a **clean git working tree**: every run needs a repo, iterations are `git commit`/`git reset --hard` boundaries, and its two concurrency modes (`--worktree`, `--current-branch`) are both git constructs. That's the right default for coding agents and the wrong universal primitive for a scraping bot that has no repo at all.
- **Fix: make "workspace" a pluggable interface, with git-checkpoint as one implementation, not the foundation.** A `Workspace` interface owns "snapshot before iteration" / "commit or roll back after iteration" / "diff since last snapshot" — `GitWorkspace` (gnhf's model, for coding tasks), `NoopWorkspace` (scraping/API-calling tasks with no meaningful filesystem state to checkpoint — isolation falls back to iteration-level idempotency and the agent's own external side effects), and later `FileSnapshotWorkspace` (tar/copy a working directory each iteration for non-git file-manipulation tasks) all satisfy the same interface. This is the single most important divergence from gnhf and it's what actually makes syla "universal" rather than "gnhf with a different agent roster." Task-file frontmatter picks the workspace kind explicitly (`workspace: git | none | file-snapshot`) rather than syla assuming a repo exists.

---

## 2. Feasibility assessment

### Solidly feasible (this is the 80% that makes syla worth building)
- **Process-level agent adapters.** Every agent you listed (Claude Code, Codex CLI, Gemini CLI, and most others) is operable as a subprocess with stdin/stdout, or exposes a "headless"/"print"/"non-interactive" or JSON-streaming mode. Wrapping `os/exec` with a well-designed `Agent` interface is standard Go systems work.
- **Looping + checkpoint/resume.** Writing loop state (iteration count, last output, task file hash, status) to a local SQLite DB or append-only JSONL log after every step is straightforward and gives you crash-safe resume almost for free.
- **A gorgeous TUI.** Bubble Tea + Lip Gloss (charmbracelet) is exactly built for this — live-updating panes, scrollable agent output, spinners, progress bars. This is a solved problem in Go's ecosystem.
- **Two entrypoints sharing one core.** `syla run task.md` and `import "syla/loop"` both calling the same `loop.Engine` is normal Go library design — `cmd/syla` is a thin binary over `pkg/loop`, `pkg/agent`, `pkg/tui`.
- **Stop conditions.** Max iterations, wall-clock timeout, cost/token budget, "no more changes since last N iterations", or a custom Go/CEL/starlark predicate — all straightforward to implement as a `StopCondition` interface.
- **Config-driven agent roster.** A `README`-owned Agents table mapping name → binary → invocation mode → capabilities (JSON output? streaming? cost reporting?) is just a struct + registry.

### Feasible but genuinely hard — where the real engineering effort goes
- **Detecting "the agent is stuck / done / needs input" without agent cooperation.** Some agents (Claude Code, Codex) support explicit non-interactive/headless flags with structured (JSON) output, which makes "did this iteration succeed" tractable. Others are interactive-only TTYs (some Gemini CLI modes, some community agents like "Pi"/"Hermes" you mention — unclear what SDK maturity they have). For those, you're stuck pattern-matching on stdout, or driving a pseudo-TTY (`github.com/creack/pty`) and scraping for prompts — which is fragile and will need per-agent heuristics, not a universal parser. **Plan for a two-tier adapter contract:** `NativeAdapter` (structured, reliable) vs `PTYAdapter` (best-effort, heuristic "idle detection" via output-quiescence timers).
- **"Iteration boundary" is not a universal concept.** For a coding agent, "one loop step" might mean "one full agentic turn until it stops using tools." For a scraping bot orchestration, the user's own code defines what a step is. Solve this by making iteration boundary **caller-defined** in the SDK path (the developer's code calls `engine.Step(ctx)` explicitly) and **agent-signaled** in the task-file path (syla infers turn-end from the adapter, e.g. "process exited" or "no tool calls in last response" or a JSON `{"status":"done"}` sentinel the task file can ask the agent to emit).
- **Vague, open-ended task instructions ("figure out what to do next").** This works well for agents that already do planning internally (Claude Code, Codex both do). It's inherently non-deterministic — syla's job isn't to make the task good, it's to make sure a bad/stuck iteration doesn't corrupt the whole overnight run. This pushes you toward **iteration isolation** (see §5.4) as the actual hard requirement, more than "understanding" the task.
- **Safety/cost blowouts running unattended overnight.** A loop with no budget cap against a paid API is a real financial risk. This needs to be a first-class, on-by-default feature, not an afterthought — token/cost tracking per adapter (where the agent reports it) and a hard wall-clock + iteration ceiling always active.
- **Resuming mid-agent-turn.** If syla crashes while the agent subprocess is mid-response, you generally cannot resume "inside" that turn — you resume at the last committed iteration boundary and re-issue (or the agent's own session/thread resumption, e.g. Claude Code's `--resume`/session IDs, Codex's session resume, if the adapter supports it). Build resume around **iteration-level idempotency**, not process-level snapshotting.
- **"Keep running through PC sleep" is two different problems, and one of them is physically impossible.** True OS sleep (suspend-to-RAM) halts every process, syla's daemon included — nothing can execute during real sleep, on any OS. What's actually achievable, and what tools like gnhf/`caffeinate` do, is **preventing** sleep for the duration of the run (hold a "stay awake" lock) so the machine simply never enters that state while syla has work to do. See §7 for the concrete per-OS mechanism — worth flagging now because it changes what you should promise in the README: not "runs through sleep" but "keeps your machine from sleeping while a run is active."

### Not really feasible / out of scope — be explicit about this in the README
- **A truly universal parser of arbitrary agent stdout.** There is no way to build one heuristic that reliably detects "done" across every present and future CLI agent without any per-agent adapter code. Every new agent added to the roster requires writing (a small) adapter — syla reduces this to "implement one interface," it doesn't eliminate it.
- **Sandboxing/containing what the agent actually does.** syla can run agents in a loop; it should not attempt to also be the security boundary (i.e., don't try to reimplement Docker/firejail sandboxing inside syla itself). Recommend the user run syla itself inside a container/VM for genuinely unattended overnight runs, and document that as the supported safety story rather than building isolation into syla.
- **Guaranteeing agent output quality/correctness.** Out of scope by design — syla is an operations/reliability layer, not a supervisor of the agent's judgment.

---

## 3. Two entrypoints — concrete design

### 3.1 Entrypoint one: `syla run <task.md>`
A markdown file is both docs and machine-readable frontmatter:

```markdown
---
agent: claude          # key into the Agents registry
mode: headless         # native | pty (adapter selection override)
workspace: git         # git | none | file-snapshot — see §1a
max_iterations: 50
max_duration: 8h
budget_usd: 15.00
stop_on: "no_diff_for(3)"   # built-in StopCondition name, or a script path
working_dir: ./
resume: auto            # auto | always | never
keep_awake: true        # hold a sleep-prevention lock for the run's duration
---

# Task

Keep improving test coverage in this repo. Each iteration:
1. Run the test suite and note the coverage delta.
2. Pick the least-covered package and add meaningful tests.
3. Stop touching a file once its coverage is >90%.

Emit `STATUS: DONE` on its own line when coverage plateaus for 3 iterations in a row.
```

- Frontmatter maps 1:1 onto a `RunConfig` struct (via `gopkg.in/yaml.v3` on the frontmatter block, body passed as the raw prompt/system context).
- syla owns iteration boundaries here: for a `NativeAdapter`, boundary = one process invocation returning; for `PTYAdapter`, boundary = output-quiescence timeout OR an agent-emitted sentinel line syla is told to watch for (`STATUS: DONE`, configurable regex).
- Every iteration's full transcript, workspace diff/snapshot (produced by whichever `Workspace` implementation the run selected — git commit, file-tree copy, or nothing), and structured result get written to `.syla/runs/<run-id>/iter-<n>.json`.
- For a scraping job specifically: `workspace: none`, and the task body tells the agent where to persist rows itself (a file, a DB, an API call) — syla's job is purely to keep re-invoking the agent, track budget/iterations, and hold the sleep-prevention lock; it has no opinion on the target's storage.

### 3.2 Entrypoint two: the SDK (`import "github.com/<you>/syla/loop"`)
This is the "build light wrappers" use case — e.g. a scraping bot where the *scraping* is bespoke Go code and syla is only the reliability harness around the agent calls embedded in it.

```go
eng := loop.New(loop.Config{
    Agent:      agent.Get("codex"),
    WorkDir:    "./scraper",
    Budget:     loop.USD(5),
    OnIteration: func(ctx context.Context, r *loop.Result) error {
        // user's own code: e.g. persist scraped rows, decide next URL
        return nil
    },
})

for eng.ShouldContinue(ctx) {
    result, err := eng.Step(ctx, loop.Prompt("scrape page 12 of the listing, extract rows"))
    if err != nil {
        eng.RecordFailure(err) // feeds retry/backoff policy
        continue
    }
    // developer's own control flow decides what "next" means
}
```

- `eng.Step` is the atomic, checkpointed unit — this is where crash-resume hooks in regardless of how "vague" or bespoke the surrounding user code is.
- The **TUI attaches to any `*loop.Engine`** — whether it was constructed by the CLI's task-file parser or directly by user code — via `tui.Attach(eng)`, so a user's custom Go program still gets the "beautiful terminal screen" for free by calling one function, or gets it automatically if run under `syla exec ./my-program` (which just wraps stdout capture — optional, lower priority).

---

## 4. Package layout (Go)

```
syla/
├── cmd/syla/              # thin CLI entrypoint (cobra or urfave/cli)
│   ├── main.go
│   ├── run.go             # `syla run <task.md>`
│   ├── resume.go          # `syla resume <run-id>`
│   ├── agents.go          # `syla agents list/add/test`
│   └── status.go          # `syla status` (also default screen on bare `syla`)
├── pkg/
│   ├── agent/              # the Agent interface + registry
│   │   ├── agent.go         # type Agent interface { Start, Send, Stream, Stop, Capabilities }
│   │   ├── registry.go      # README-driven table, embed + user config merge
│   │   ├── native/          # adapters with structured/headless support
│   │   │   ├── claude.go
│   │   │   ├── codex.go
│   │   │   └── gemini.go
│   │   ├── acp/              # generic Agent Client Protocol adapter — one client, many agents
│   │   │   ├── acp.go          # ACP session lifecycle over stdio (or an acpx-style bundled runtime)
│   │   │   └── target.go       # acp:<target-or-command> spec parsing, mirrors gnhf's convention
│   │   └── pty/             # heuristic adapters for interactive-only agents
│   │       ├── pty_adapter.go
│   │       └── quiescence.go
│   ├── loop/                # the engine — the real IP of the project
│   │   ├── engine.go
│   │   ├── stopcond.go      # StopCondition interface + built-ins
│   │   ├── checkpoint.go    # SQLite/JSONL persistence
│   │   ├── budget.go        # cost/token/time tracking
│   │   └── retry.go
│   ├── taskfile/            # markdown+frontmatter parser → RunConfig
│   ├── tui/                 # Bubble Tea app, attaches to loop.Engine via channel/事件 bus
│   ├── workspace/           # Workspace interface (§1a): git, none, file-snapshot
│   │   ├── workspace.go
│   │   ├── git.go
│   │   ├── noop.go
│   │   └── filesnapshot.go
│   ├── daemon/               # detach/attach machinery — process management + IPC server
│   │   ├── daemon.go          # fork/spawn detached, PID + socket lifecycle
│   │   ├── ipc.go              # Unix domain socket (Windows: named pipe) JSON-RPC server
│   │   └── client.go           # thin client used by cmd/syla and pkg/tui
│   └── keepawake/             # OS sleep-prevention — see §7
│       ├── keepawake_darwin.go   # shells out to `caffeinate`
│       ├── keepawake_linux.go    # shells out to `systemd-inhibit`, falls back to D-Bus if absent
│       └── keepawake_windows.go  # SetThreadExecutionState via syscall
├── .syla/                   # (runtime, gitignored) run state lives here per-project
├── README.md                # Agents table lives here — single source of truth
└── go.mod
```

Key design call: **`loop.Engine` emits events on a channel** (`IterationStarted`, `IterationCompleted`, `BudgetWarning`, `StopConditionMet`, `Failure`), and both the CLI's TUI and a user's own code (SDK path) subscribe to the same event stream. This is what makes "one engine, two entrypoints, always the same guarantees" actually true rather than aspirational.

---

## 5. Loop engine internals — the part to get right first

### 5.1 State machine
`Idle → Running → (Paused | Failed | Completed)`, with `Running` looping through `IterationStart → AgentInvoke → IterationEnd → EvalStopConditions`.

### 5.2 Persistence
- SQLite (via `mattn/go-sqlite3` or pure-Go `modernc.org/sqlite` to avoid cgo) for run metadata + iteration index.
- Raw transcripts as flat files (`.syla/runs/<id>/iter-N/{prompt.md,output.txt,diff.patch,meta.json}`) — keeps the DB small and lets users `cat`/`grep` runs directly, which matters a lot for debugging an overnight run in the morning.

### 5.3 Resume semantics
`syla resume <run-id>` reloads the last committed `RunState`, re-attaches the same `Agent` (using the adapter's own session-resume feature when available — e.g. Claude Code and Codex both support resuming a session by ID, which is strictly better than re-prompting from scratch), and continues iteration numbering from where it left off.

### 5.4 Iteration isolation (the actual answer to "vague tasks failing safely")
Because the task can be open-ended, the engine should not assume iteration *n+1* can safely build on a broken iteration *n*. Two knobs to expose:
- `isolation: none` — iterations share full working-directory state (default, cheapest).
- `isolation: git-checkpoint` — auto-commit or `git worktree` snapshot at each iteration boundary, so a bad iteration can be auto-reverted by a stop condition (`stop_on: "tests_failed_after_revert_attempt"`) without losing the whole run.

### 5.5 Stop conditions (built-ins to ship day one)
`max_iterations(n)`, `max_duration(d)`, `budget(usd)`, `no_diff_for(n)` (workspace-aware — asks the active `Workspace` for a diff, so this degrades gracefully to "always false" under `workspace: none`), `output_matches(regex)`, `custom(path/to/script)` (invoked as its own subprocess, receives the run's JSON state on stdin, returns exit code 0 = stop).

### 5.6 Rate-limit / usage-window waits — treat separately from failures
Borrowed directly from gnhf because it's a real, common failure mode for overnight runs against subscription-tier agent CLIs: when an adapter reports a quota/rate-limit rejection (not a generic error), the engine should not count it toward the consecutive-failure abort threshold. Instead: roll back the iteration (workspace-dependent — a no-op under `workspace: none`), read a reset time from the adapter's error payload if it provides one, sleep until shortly after that time (capped by a configurable `max_rate_limit_wait`, default 24h, after which the run aborts cleanly rather than waiting indefinitely), and retry the same iteration. This needs its own `RateLimited` error type in the `Agent` interface's contract so adapters can signal it distinctly from a generic failure.

---

## 6. TUI

- **Framework:** Bubble Tea + Lip Gloss + Bubbles (for the viewport/spinner/progress components) — this is the de facto standard for exactly this kind of tool in Go (see: `lazygit`, `gh dash`).
- **Default screen on bare `syla`** (per your CLAUDE.md line "opens up as soon as a terminal is started" — clarify whether you mean *as soon as you type `syla` with no args* or *literally on shell startup via a hook*; the former is far more standard and is what's assumed here):
  - No active run → dashboard of past runs (status, duration, cost) + "start new run" prompt.
  - Active run → live pane: current iteration number, elapsed time, budget spent/remaining, streaming agent output (scrollable), stop-condition status bar.
- Keybinds: `p` pause, `r` resume, `q` quit-TUI-but-keep-run-alive-in-background (needs the engine running as a detached process or the TUI to just be a client over a local socket/IPC — see §7).

---

## 7. Daemon architecture + surviving sleep — confirmed design

You've confirmed: detached daemon, and it needs to keep going even when the machine would otherwise sleep. Two separate mechanisms, both required:

### 7.1 Detach: daemon process + IPC, TUI as a thin client
`syla run task.md` does **not** run the loop in the foreground process. It:
1. Validates the task file, writes initial `RunState` to `.syla/runs/<run-id>/`.
2. Forks/spawns a detached background process (`syscall.SysProcAttr{Setsid: true}` on Unix so it survives the parent's terminal closing/SIGHUP; on Windows, `CREATE_NEW_PROCESS_GROUP` + `DETACHED_PROCESS` creation flags) running the actual `loop.Engine`.
3. That daemon process opens a Unix domain socket (`.syla/runs/<run-id>/ctl.sock`; a named pipe on Windows, since Windows has no Unix sockets pre-Go 1.12-ish parity concerns — Go's `net` package handles this transparently via `winio` or the stdlib's now-native named-pipe support) and serves a small JSON-RPC/gob API: `Status`, `Subscribe` (streams the event bus from §4), `Pause`, `Resume`, `Stop`.
4. The foreground `syla run` process becomes the **first TUI client**, connecting immediately over that socket — so the common case ("I typed the command and I'm watching it") looks identical to a naive in-process implementation, it's just already talking over IPC.
5. Closing the terminal / Ctrl+C on the TUI sends a "detach" (not "stop") by default — the daemon keeps running. `syla attach <run-id>` (or bare `syla`, showing a picker if multiple runs are active) reconnects a fresh TUI client later. An explicit `syla stop <run-id>` is required to actually kill the loop.
6. **Surviving a full logout/reboot** (as opposed to just terminal-close) is a step beyond a detached process — `setsid`/`DETACHED_PROCESS` protects against the terminal dying, but a user logout can still SIGHUP or kill session-scoped processes on some OS configurations. If you want runs to survive that too, the daemon needs to register as a real OS-level background service: a `launchd` user agent (`~/Library/LaunchAgents/*.plist`) on macOS, a `systemd --user` unit on Linux, or a Windows Task Scheduler entry / service on Windows — `syla run --persist` could generate and load the right one automatically. Treat this as a stretch goal (§8 phase 7), not part of the core daemon work, since the plain detached-process model already satisfies "survives closing the terminal and putting the laptop to sleep."

### 7.2 Keep-awake: preventing sleep, not running through it
As noted in §2, real suspend halts everything — so the daemon, on starting a run with `keep_awake: true` (default), acquires an OS-level "inhibit sleep" lock for the run's lifetime and releases it on completion/stop. This is exactly what gnhf does per-OS, and it's the correct, portable answer:
- **macOS:** shell out to `caffeinate -dims` (or hold the equivalent `IOPMAssertionCreateWithName` via cgo if you want to avoid spawning a subprocess — starting with the `caffeinate` subprocess is simpler and is what gnhf ships).
- **Linux:** `systemd-inhibit --what=sleep:idle --who=syla --why="run <id> in progress" --mode=block <daemon-command>` (this typically means re-exec'ing the daemon under `systemd-inhibit` rather than calling it from within, since the inhibitor lock is tied to the process it wraps); on non-systemd setups, fall back to a D-Bus call to `org.freedesktop.PowerManagement` or document that keep-awake isn't available and the run may be interrupted by sleep.
- **Windows:** `SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED | ES_AWAYMODE_REQUIRED)` via `golang.org/x/sys/windows` — no subprocess needed, this is a direct syscall.
- **Never abort a run because the keep-awake mechanism failed to acquire** — log it prominently (daemon log + TUI status line + the final exit summary, mirroring gnhf's behavior) and let the run proceed at risk of interruption, since a failed lock is far less bad than a silently-dead overnight run the user finds out about after paying for it.
- This is exactly why keep-awake needs to be a daemon-level concern, not a TUI concern: if the lock were held by the TUI client process, detaching (§7.1) would release it. The daemon acquires it when the run starts and holds it regardless of how many TUI clients come and go.

---

## 8. Suggested build order (phases, not hard sprints)

1. **`pkg/agent` + one native adapter (Claude Code headless mode) + `pkg/workspace` (start with `NoopWorkspace` + `GitWorkspace`) + `pkg/loop` core with in-process execution and SQLite checkpointing.** No daemon, no TUI yet — prove loop/resume/stop-condition/workspace logic with plain log output.
2. **`pkg/taskfile` + `cmd/syla run`** — first entrypoint fully working end-to-end, still in-process.
3. **`pkg/daemon` (detach + Unix-socket/named-pipe IPC) + `pkg/keepawake` (start with macOS/Linux since that's most likely your dev environment, add Windows once the interface is proven).** Convert the engine to run detached by default, add `attach`/`status`/`stop` commands. This is the highest-risk refactor; doing it in phase 3 while the surface area is still small is much cheaper than doing it in phase 6.
4. **TUI** (Bubble Tea client over the daemon's IPC) — this is now "just" a renderer for events the engine already emits.
5. **SDK entrypoint** (`pkg/loop` public API polish + docs + example scraping-bot in `examples/`, using `workspace: none`) — validates the "light wrapper" use case concretely, and is your first real test that syla isn't secretly still git-coupled.
6. **Second/third native adapters** (Codex, Gemini) + `pkg/agent/acp` (the ACP client, giving you the `acp:<target>` escape hatch confirmed as required) + first `PTYAdapter` for a non-headless agent — proves all three adapter kinds against the same `Agent` interface; write native ones against gnhf's Agents table (§1a) as a reference for what each CLI's non-interactive/JSON flags actually are.
7. **`FileSnapshotWorkspace` + rate-limit-wait handling (§5.6) + budget tracking polish + `syla agents add` + the `--persist` OS-service registration stretch goal (§7.1).**

---

## 9. Things to decide before writing code (so the plan doesn't drift)
- Exact meaning of "as soon as a terminal is started" for the TUI (shell-hook vs bare-command — strongly recommend bare-command).
- Whether "Pi" and "Hermes" have any non-interactive/scriptable mode at all — if not, they start life as `PTYAdapter`s and that should be stated plainly in the README Agents table (a "Mode" column: `native` / `pty`) rather than implied to be equal in reliability to Claude Code/Codex. Check gnhf's own adapter for Pi (`--agent pi`, JSON mode with `--no-session`) as a working reference before assuming it needs PTY treatment.
- License and adapter-contribution model (since the Agents table is meant to grow via community/your own additions — a documented `Agent` interface + a `CONTRIBUTING.md` with an adapter template matters as much as the code).
- Whether `syla run --persist` (true OS-service registration, §7.1) is worth the three-times-over platform-specific work before v1, or whether "detached process + keep-awake lock" is a good enough answer to "overnight" for the first release — it covers laptop-open-overnight and terminal-closed cases, just not full logout/reboot.
