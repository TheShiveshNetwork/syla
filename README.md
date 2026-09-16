# syla — see you later, alligator

syla runs any AI agent in a supervised, resumable loop so it can work unattended — overnight, or until a budget, an iteration count, or a condition is met — without the run silently dying if your terminal closes or your laptop tries to sleep.

It has two entrypoints:
- `syla run <task.md>` — pass a markdown file describing the objective and any procedure, syla loops an agent against it.
- `import "github.com/<you>/syla/loop"` — build your own program (a scraper, a data pipeline, anything) and use syla's loop engine as the reliability harness around your own agent calls.

Full architecture — the daemon/IPC model, the keep-awake mechanism, workspace isolation, stop conditions — lives in [`docs/architecture.md`](docs/architecture.md). This README covers what you need to install, run, and configure syla day to day. Contribution workflow and coding conventions live in [`AGENTS.md`](AGENTS.md).

## Quick start

```
syla run task.md
```

```
syla run task.md --max-iterations 20 --budget-usd 10
```

```
syla attach <run-id>    # reattach a TUI to a run you detached from earlier
syla status              # list active and recent runs
syla stop <run-id>       # stop a run for good
```

A run keeps going after you close the terminal — closing the TUI detaches, it does not stop the loop. See [Daemon and detaching](#daemon-and-detaching).

## Install

**From source**
```
git clone https://github.com/<you>/syla.git
cd syla
go build -o syla ./cmd/syla
```

**Go install**
```
go install github.com/<you>/syla/cmd/syla@latest
```

syla supports macOS, Linux, and Windows. Keep-awake support (holding your machine open during a run) is native on all three — see [Sleep prevention](#sleep-prevention).

## Task files

A task file is a markdown file with YAML frontmatter for the run's configuration and a markdown body for the objective:

```markdown
---
agent: claude
workspace: git
max_iterations: 50
max_duration: 8h
budget_usd: 15.00
stop_on: "no_diff_for(3)"
---

# Task

Keep improving test coverage in this repo. Stop touching a file once its coverage is >90%.
```

| Field | Values | Default | Notes |
| --- | --- | --- | --- |
| `agent` | any name from the Agents table below | — | required |
| `mode` | `native` \| `acp` \| `pty` | agent's default | overrides how syla talks to the agent |
| `workspace` | `git` \| `none` \| `file-snapshot` | `none` | how iteration boundaries snapshot/roll back state — use `git` for coding tasks in a repo, `none` for anything else (e.g. a scraping job) |
| `max_iterations` | integer | unlimited | |
| `max_duration` | duration (`8h`, `30m`) | unlimited | |
| `budget_usd` | decimal | unlimited | aborts the run once reported spend crosses this |
| `stop_on` | built-in name or script path | none | see Stop conditions in the architecture doc |
| `resume` | `auto` \| `always` \| `never` | `auto` | |
| `keep_awake` | boolean | `true` | hold a sleep-prevention lock for the run's duration |

## Agents

syla talks to an agent one of three ways. `native` adapters call an agent's own headless/non-interactive mode directly and get structured, reliable results. `acp` speaks the [Agent Client Protocol](https://agentclientprotocol.com) over stdio, which works with any ACP-compliant agent without a bespoke adapter. `pty` drives an interactive-only agent through a pseudo-terminal and infers iteration boundaries heuristically (output quiescence, or a sentinel line the task asks the agent to emit) — it's the fallback for agents with no scriptable mode, and is less reliable than the other two.

| Agent | Flag | Mode | Requirements |
| --- | --- | --- | --- |
| Claude Code | `--agent claude` | native | `claude` CLI installed and signed in |
| Codex | `--agent codex` | native | `codex` CLI installed and signed in |
| Gemini | `--agent gemini` | native or acp | `gemini` CLI installed, or reachable via `acp:gemini` |
| Pi | `--agent pi` | native | `pi` CLI installed with a configured provider |
| Hermes | `--agent hermes` | pty (pending an adapter contribution) | see [Contributing an agent](#contributing-an-agent) |
| any ACP target | `--agent acp:<target-or-command>` | acp | the target must speak ACP; a quoted custom command is also accepted |

Custom binary paths and per-agent extra flags are set in `~/.syla/config.yml` via `agentPathOverride` and `agentArgsOverride`, keyed by agent name.

### Contributing an agent
Implement the `Agent` interface (`pkg/agent`) for the agent's native invocation if it has one, or confirm it speaks ACP and use the existing `acp` client with no new code, or fall back to a `pty` adapter as a last resort. Add a row to the table above and a short note on required setup. See `AGENTS.md` for code conventions.

## Daemon and detaching

`syla run` starts the loop in a detached background process and connects the terminal you ran it from as the first TUI client. Ctrl+C or closing the terminal detaches — the run keeps going. `syla attach <run-id>` reconnects a TUI to it later, and `syla stop <run-id>` is the only thing that actually ends it. `syla status` lists every active and recent run with its state, elapsed time, and spend so far.

## Sleep prevention

By default (`keep_awake: true`) syla holds an OS-level lock that stops your machine from sleeping for as long as a run is active — it does not, and cannot, run anything during genuine suspend-to-RAM sleep, so this is what actually delivers "runs overnight." The mechanism is native per OS: `caffeinate` on macOS, `systemd-inhibit` on Linux (falling back to a warning if neither systemd nor a D-Bus power-management service is available), and `SetThreadExecutionState` on Windows. If the lock can't be acquired, the run proceeds anyway and the exit summary says so — a failed lock is logged, never a reason to abort a run.

## SDK

```go
eng := loop.New(loop.Config{
    Agent:   agent.Get("codex"),
    Workspace: workspace.None(),
    Budget:  loop.USD(5),
})

for eng.ShouldContinue(ctx) {
    result, err := eng.Step(ctx, loop.Prompt("scrape page 12, extract rows"))
    if err != nil {
        eng.RecordFailure(err)
        continue
    }
    // your own code: persist rows, decide the next URL, whatever the task needs
}
```

`tui.Attach(eng)` gets you the same terminal UI as the CLI entrypoint from inside your own program, and works against any `*loop.Engine` whether it was built by a task file or by your own code.

## Configuration file

`~/.syla/config.yml`, created on first run:

```yaml
agent: claude
agentPathOverride:
  claude: ~/bin/claude-code-switch
agentArgsOverride:
  codex:
    - -c
    - model_reasoning_effort="high"
maxConsecutiveFailures: 3
keepAwake: true
```

CLI flags override this file for a single run; `agent`, `agentPathOverride`, and `agentArgsOverride` persist here.

## Architecture

See [`docs/architecture.md`](docs/architecture.md) for the full design: the loop state machine, `Workspace` and `StopCondition` interfaces, the daemon/IPC protocol, rate-limit-wait handling, and the phased build plan.

## Contributing

See [`AGENTS.md`](AGENTS.md) for coding conventions and [`CONTRIBUTING.md`](CONTRIBUTING.md) for the workflow.
