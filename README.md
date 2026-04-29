# workmuxinator

Minimal workflow runner in Go.

`workmuxinator` executes DAG workflows from YAML and can run:

- AI nodes (`prompt` or `prompt_file`)
- deterministic shell nodes (`bash`)
- loop nodes with stop conditions (`ALL_TASKS_COMPLETE`, `APPROVED`)
- explicit failure fallback edges (`on_failure`)

It is intentionally small so you can plug any coding agent and define your own orchestration flow.

Legacy compatibility is also built in: tmuxinator/workmux launch, resume, add, and rm commands are preserved.

## Goals

- Agent-agnostic: use Codex, Claude, Aider, Cursor agent, or custom commands.
- DAG-first: express dependencies with `depends_on`.
- Prompt-as-file: keep prompts in plain text files.
- Human gates: interactive approval loops.
- Failure routing: fallback node per step (`on_failure`).

## Install

```bash
git clone https://github.com/lcensies/workmuxinator
cd workmuxinator
make build
sudo make install
```

## Quick start

1. Configure agent command in `.workmuxinator/agent.yaml`:

```yaml
command: codex
args: []
prompt_flag: ""
fresh_context_arg: ""
```

2. Run the sample workflow:

```bash
workmuxinator run -f .workmuxinator/workflows/build-feature.yaml
```

3. Validate workflow schema:

```bash
workmuxinator validate -f .workmuxinator/workflows/build-feature.yaml
```

## Workflow format

```yaml
nodes:
  - id: plan
    prompt_file: ../prompts/plan.txt

  - id: implement
    depends_on: [plan]
    loop:
      prompt_file: ../prompts/implement.txt
      until: ALL_TASKS_COMPLETE
      fresh_context: true
      max_iterations: 10
    on_failure: rollback

  - id: run-tests
    depends_on: [implement]
    bash: bun run validate
    on_failure: rollback

  - id: approve
    depends_on: [run-tests]
    loop:
      prompt_file: ../prompts/approve.txt
      until: APPROVED
      interactive: true
      max_iterations: 20

  - id: rollback
    prompt_file: ../prompts/rollback.txt
```

### Node keys

- `id`: unique node name.
- `depends_on`: list of prerequisite nodes.
- `prompt`: inline AI prompt.
- `prompt_file`: path to prompt text file (relative to workflow file).
- `bash`: deterministic shell command.
- `loop`: iterative AI execution.
- `on_failure`: node to execute when this node fails.

### Loop keys

- `prompt` or `prompt_file`: required.
- `until`: `ALL_TASKS_COMPLETE` or `APPROVED`.
- `fresh_context`: appends agent-specific fresh-context arg if configured.
- `interactive`: prompts for human `y/N` confirmation each iteration.
- `max_iterations`: safety cap (default `8`).

## CLI

```bash
workmuxinator                      # legacy open all projects
workmuxinator open                 # legacy open all projects
workmuxinator run --resume         # legacy resume mode
workmuxinator add <dir>            # legacy add tmuxinator config
workmuxinator rm <dir>             # legacy remove tmuxinator config

workmuxinator workflow run [flags]
workmuxinator run -f ...           # workflow shortcut
workmuxinator workflow validate [flags]
workmuxinator validate [flags]     # workflow shortcut
workmuxinator version
workmuxinator help
```

Workflow run flags:

- `-f`: workflow path (default `.workmuxinator/workflows/build-feature.yaml`)
- `--agent`: override agent command
- `--prompt-flag`: override prompt flag for agent
- `--fresh-context-arg`: override fresh context arg
- `--dry-run`: print execution plan without commands

Built-in workflow agent defaults:

- `opencode`: uses `--prompt` automatically unless `prompt_flag` is explicitly configured.
- `cursor-agent`: uses positional prompts by default (no prompt flag required).

## Integration notes

- Orchestrator-friendly: use `bash` nodes to call tools like `workmux`, `oh-my-cursor`, CI scripts, or custom wrappers.
- Prompt catalogs: keep shared prompt packs in `.workmuxinator/prompts/` and reference them from multiple workflows.
- Failure control: route failed nodes into recovery nodes (`rollback`, `repair`, `retry`) via `on_failure`.
