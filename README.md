# workmuxinator

**workmuxinator** is a small bash wrapper that bridges [tmuxinator](https://github.com/tmuxinator/tmuxinator) and [workmux](https://github.com/cablehead/workmux). It reads your tmuxinator project configs, finds every project's root, and opens the existing workmux worktrees there—optionally resuming your AI coding agent (Claude Code, aider, …) in each one.

Think of it as an "open everything" button for your multi-project, multi-agent workflow.

---

## How it works

```
tmuxinator configs          workmux per-project config
~/.config/tmuxinator/       .workmux.yaml / ~/.config/workmux/config.yaml
        │                               │
        ▼                               ▼
  project root            which agent? claude / aider / …
  list of windows                       │
        │                               │ (resume flags)
        └──────────┬────────────────────┘
                   ▼
           workmux open <worktree>
           workmux run  <worktree> -- <agent> --resume --continue ["continue if not completed"]
```

1. **Scan** – finds every `*.yml` file in `~/.config/tmuxinator/` (skipping the built-in `--help.yml`).
2. **Resolve** – reads the `root:` key from each config and expands `~`.
3. **List** – runs `workmux list` inside that root to discover existing worktrees.
4. **Open** – calls `workmux open <name> --new` for each worktree (creates it with `workmux add` if it doesn't exist yet).
5. *(run only)* **Resume** – detects the configured agent and appends the correct resume flags (and optionally a continuation prompt with `--resume`), then delivers the command via `workmux run <name>`.

---

## Installation

### Quick (any distro)

```bash
git clone https://github.com/lcensies/workmuxinator
cd workmuxinator
sudo make install          # installs to /usr/local/bin
```

Override the prefix:

```bash
make install PREFIX=~/.local
```

### Debian / Ubuntu

```bash
make deb
sudo dpkg -i workmuxinator_0.1.0_all.deb
```

### Fedora / RHEL / openSUSE

```bash
make rpm
sudo rpm -i ~/rpmbuild/RPMS/noarch/workmuxinator-0.1.0-1.noarch.rpm
```

### Arch Linux

```bash
cd packaging/arch
makepkg -si
```

### Nix / NixOS

```bash
# Run without installing
nix run github:lcensies/workmuxinator

# Install into profile
nix profile install github:lcensies/workmuxinator

# Use in a flake
inputs.workmuxinator.url = "github:lcensies/workmuxinator";
```

---

## Requirements

| Tool | Role | Notes |
|------|------|-------|
| `tmux` | Terminal multiplexer | Required |
| `tmuxinator` | Project config source | Required |
| `workmux` | Worktree / agent orchestrator | Required |
| `yq` (Go) | Robust YAML parsing | Recommended – falls back to `awk`/`sed` without it |

---

## Usage

```
workmuxinator               Open workmux worktrees for all tmuxinator projects
workmuxinator run           Open worktrees and resume the AI agent in each
workmuxinator run --resume  Same, plus send "continue if not completed" prompt
workmuxinator add DIR       Register a directory as a tmuxinator project
workmuxinator rm DIR        Remove a directory's tmuxinator project config
workmuxinator version       Print version
workmuxinator help          Print help
```

### `workmuxinator`

Iterates every tmuxinator project config, enters its root directory, and opens
all existing workmux worktrees in new tmux windows. Worktrees that don't exist
yet are created with `workmux add --background`.

### `workmuxinator run`

Same as the default command, but after opening each worktree it also resumes
the last agent session inside it:

```
claude         →  claude --resume --continue
opencode       →  opencode --continue
cursor-agent   →  cursor-agent   (pass prompt directly as positional argument)
aider          →  aider   (resumes via .aider.chat.history.md automatically)
custom         →  <agent>  (no extra flags; configure via .workmux.yaml)
```

### `workmuxinator run --resume`

Same as `run`, but also sends a continuation prompt to each agent after
resuming. This tells in-progress agents to keep going without waiting for
manual input:

```
claude         →  claude --resume --continue "continue if not completed"
opencode       →  opencode --continue --prompt "continue if not completed"
cursor-agent   →  cursor-agent "continue if not completed"
aider          →  aider   (prompt not applicable; still resumes via history)
custom         →  <agent>  (no prompt; no extra flags)
```

---

### `workmuxinator add DIR`

Creates a minimal tmuxinator config for the given directory, using its
basename as the project name:

```bash
workmuxinator add ~/projects/myapi
# → creates ~/.config/tmuxinator/myapi.yml
```

The generated config sets `root:` to the resolved absolute path. You can
edit the file afterwards to add windows or other tmuxinator options.

Fails if a config with that name already exists.

### `workmuxinator rm DIR`

Removes the tmuxinator config whose `root:` matches the given directory.
Falls back to matching by the directory's basename if no root match is found.

```bash
workmuxinator rm ~/projects/myapi
# → removes ~/.config/tmuxinator/myapi.yml
```

---

### tmuxinator project config

workmuxinator only needs the `root:` field from your tmuxinator YAML:

```yaml
# ~/.config/tmuxinator/myproject.yml
name: myproject
root: ~/projects/myproject

windows:
  - editor: vim
  - tests: pytest --watch
```

The `windows:` section is parsed but currently used informally—workmux itself
manages window creation. The key field is `root:`.

### workmux agent config

workmuxinator reads the `agent:` key from:

1. `<project-root>/.workmux.yaml`
2. `~/.config/workmux/config.yaml`
3. Fallback: `claude`

Example `.workmux.yaml`:

```yaml
agent: claude
# or:
# agent: aider
# agent: /usr/local/bin/my-agent
```

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TMUXINATOR_CONFIG_DIR` | `~/.config/tmuxinator` | Where tmuxinator project YAMLs live |

---

## Example workflow

```bash
# 1. Create a tmuxinator project for each repo you work on
tmuxinator new myapi
tmuxinator new frontend

# 2. Create some workmux worktrees in those repos
cd ~/projects/myapi
workmux add feature/auth
workmux add fix/rate-limiting

# 3. Next morning: restore everything and resume agents
workmuxinator run
# → opens feature/auth and fix/rate-limiting windows for myapi
# → runs `claude --resume --continue` in each

# Or, to also nudge in-progress agents to keep going:
workmuxinator run --resume
# → runs `claude --resume --continue "continue if not completed"` in each
```

---

## Project layout

```
bin/workmuxinator          Main script
Makefile                   install / packaging targets
flake.nix                  Nix flake
packaging/
  debian/control           dpkg-deb metadata
  rpm/workmuxinator.spec   rpmbuild spec
  arch/PKGBUILD            Arch makepkg
.github/workflows/
  release.yml              Build & attach packages on GitHub release
tmuxinator/
  workmuxinator.yml        tmuxinator config for this repo (for testing)
```
