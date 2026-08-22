# AVO Harness

**AVO Harness is an unofficial open-source implementation of a long-horizon agent loop inspired by NVIDIA's published AVO architecture. It is not an NVIDIA project and does not contain NVIDIA's internal AVO implementation.**

The harness sits above a coding agent and turns repeated agent runs into a persistent search process:

```text
objective
   |
   v
persistent orchestrator ---- episodic memory
   |                              ^
   v                              |
candidate worktree -> evaluators -+
   |                    |
   +---- improved? -----+---- no progress ----> supervisor
   |                                             |
   +---------------- next iteration <------------+
```

Each attempt starts from the best known Git commit in an isolated worktree. The worker edits the repository, the harness commits the candidate, deterministic evaluators score it, SQLite stores the lineage and evidence, and the next attempt receives compact memory from previous rounds. Repeated stagnation wakes a separate supervisor pass that injects a new strategy without editing the target repository.

## What works in v0.1

- Persistent run, candidate, evaluator, and intervention history in SQLite.
- Git worktree isolation and explicit candidate lineage.
- Weighted deterministic evaluators with either exit-code scoring or a one-line JSON score protocol.
- Automatic promotion of better candidates and a stable `avo/<run>/best` result branch.
- Episodic memory assembled from recent attempts and evaluator evidence.
- Stagnation detection plus a supervisor agent with cooldown.
- A generic command worker for any CLI coding agent.
- A native NVIDIA NeMo Fabric worker using its typed Python SDK and local workspace contract.
- Hermes, Codex, Claude, or another Fabric adapter can be selected through `worker.adapter_id` when the corresponding Fabric adapter is installed.

This deliberately leaves scheduling, distributed workers, semantic memory, and tree search out of the first cut. The core loop is small enough to reason about before those are layered on.

## Install

Core command-worker mode has no runtime dependencies beyond Python 3.11+ and Git:

```bash
python -m pip install -e .
```

For NeMo Fabric:

```bash
python -m pip install -e '.[nemo]'
```

Install the Fabric adapter/harness you intend to use as described by NVIDIA. For example, Hermes uses the `nvidia.fabric.hermes` adapter; Codex uses `nvidia.fabric.codex`; Claude uses its corresponding Fabric adapter.

## Configure

Generate a starting config:

```bash
avo-harness init --repo /path/to/project --output avo.json
```

The generated config uses NeMo Fabric + Hermes and a `pytest -q` evaluator. A minimal command-worker config looks like this:

```json
{
  "repo": "/path/to/project",
  "state_dir": "~/.local/state/avo-harness",
  "max_iterations": 12,
  "acceptance_score": 1.0,
  "worker": {
    "backend": "command",
    "command": ["your-agent", "--non-interactive"]
  },
  "supervisor": {
    "enabled": true,
    "stagnation_rounds": 2,
    "cooldown_rounds": 2
  },
  "evaluators": [
    {
      "name": "tests",
      "command": "pytest -q",
      "weight": 1.0
    }
  ]
}
```

A command worker receives the full iteration prompt on stdin unless its command contains `{prompt}` or `{prompt_file}`. It also receives `AVO_PROMPT` and `AVO_WORKSPACE` environment variables. `{workspace}` can be used in command arguments.

### NeMo Fabric worker

```json
{
  "worker": {
    "backend": "nemo",
    "adapter_id": "nvidia.fabric.hermes",
    "provider": "nvidia",
    "model": "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning",
    "api_key_env": "NVIDIA_API_KEY",
    "base_url": "https://integrate.api.nvidia.com/v1",
    "max_turns": 24
  }
}
```

The integration uses `EnvironmentConfig(provider="local", workspace=...)`, so every candidate worktree is the workspace visible to the selected Fabric harness.

## Evaluator protocol

An evaluator is any shell command. By default, exit code `0` scores `1.0` and a nonzero exit code scores `0.0`.

For partial credit, print a JSON object as the final non-empty stdout line:

```json
{"score": 0.72, "summary": "18 of 25 benchmark cases pass"}
```

`score` is clamped to `[0, 1]`. Multiple evaluator scores are combined by weight. This lets tests, lint, benchmarks, static analysis, or task-specific judges contribute independently.

## Run

First validate the environment:

```bash
avo-harness doctor -c avo.json
```

Then start a run:

```bash
avo-harness run -c avo.json "Fix the failing authentication flow without regressing the API contract"
```

The target repository must be clean by default. Each candidate lives on its own `avo/<run-id>/i-####` branch. At the end, the best commit is also pointed to by `avo/<run-id>/best`; the harness never merges it into your main branch automatically.

Inspect the latest persisted run:

```bash
avo-harness status -c avo.json
```

## How the AVO-style loop maps

| AVO-style concept | This implementation |
| --- | --- |
| Long-running main agent | repeated worker invocations against the best candidate |
| Persistent memory | SQLite trajectory + compact episodic prompt memory |
| Environment/tool use | real Git worktree exposed to the coding harness |
| External feedback | deterministic weighted evaluators |
| Supervisor intervention | isolated supervisor pass after configurable stagnation |
| Search/variation | candidate branches descending from the best known commit |
| Durable result | best commit + result branch + complete evidence trail |

## Safety / operational boundaries

The worker is a coding agent with whatever filesystem/tool permissions its backend provides. Run it in a disposable clone or sandbox when working with untrusted repositories or powerful agent configurations. Evaluator commands are also arbitrary shell commands from your config.

The harness intentionally does **not** auto-merge, push, deploy, or mutate the target branch. It creates Git branches and linked worktrees only.
