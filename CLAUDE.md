# gittree

A k9s-style TUI that finds every git repository under a directory and shows
them as one scrollable tree: local branches, their state against upstream, and
what is uncommitted. Read-only; `enter` hands the terminal to lazygit.

## Go skills

Load `schretzi/schretzi-skills@golang-how-to` at the start of any Go coding,
review, debugging, or setup task here, and let it route to whatever the task
needs. These are reference material, not law: where a skill and the rules
below disagree, the rules below win, because they were derived from how this
program actually has to behave.

Worth loading for most changes here:

- `schretzi/schretzi-skills@golang-concurrency` — the scan and fetch pools,
  and the rule about which context cancels what.
- `schretzi/schretzi-skills@golang-lint` — `.golangci.yml` is the source of
  truth for enabled linters; `make pipeline` must stay green.
- `schretzi/schretzi-skills@golang-spf13-cobra` — the CLI is a cobra command
  tree; anything touching commands, flags, args or completion belongs there.

If a skill turns out to be wrong or missing something, fix it at the source
(`~/Workspace/Schretzi/schretzi-skills`) rather than working around it here.

## Licensing

MIT, Copyright (c) 2026 Martin Fuchsluger. No per-file SPDX headers.

## Dependencies

**The Charm v2 libraries live at `charm.land`, not `github.com/charmbracelet`.**
The GitHub path resolves through the proxy but the published module declares
`charm.land/bubbletea/v2`, and that is what imports must say.

Bubble Tea v2 is not v1 with a new import path. Anything written from v1 memory
will not compile:

- `View()` returns `tea.View`, a struct, not a `string`. The alternate screen
  is `View.AltScreen`, not a program option — `WithAltScreen()` does not exist.
- `tea.KeyMsg` is an interface; switch on the concrete `tea.KeyPressMsg`.
- `bubbles/v2/help` uses `SetWidth(w)`, not a `Width` field.
- `lipgloss/v2` takes an `image/color.Color`; `AdaptiveColor` is gone, replaced
  by `HasDarkBackground` plus `LightDark`.

Check pkg.go.dev before writing against any of these from memory.

## Project rules

- **Nothing outside `internal/tui` may import Bubble Tea**, and `tui` must not
  run subprocesses. That separation is why the whole data path — discovery,
  the git parsers, path compaction — is testable with no terminal, and why the
  UI is testable with no git. Keep it.
- **`Update` is a pure function of `(model, message)`.** Side effects are
  injected as `tui.Commands` factories, built in `internal/cli`. This is also
  why the model never holds a `context.Context`: the CLI closes over it when
  building the factories.
- **Every read-only git invocation passes `--no-optional-locks`.** Without it
  gittree takes `index.lock` while it reads and fights whatever the user is
  doing in a shell in the same repository. Non-negotiable.
- **The `for-each-ref` format is NUL-separated (`%00`), not `|`.** A branch
  name may legally contain `|`. It may not contain NUL or a newline.
- **A background fetch must never be able to block on input.** `hardenedEnv`
  disables every prompt, and `Fetch` kills the whole process group on timeout.
  Killing only `git` leaves the `ssh` it spawned holding the pipes, so `Wait`
  blocks past the timeout — `TestFetchTimeoutIsActuallyEnforced` fails if that
  kill is removed, which is the point of it. Do not "simplify" it away.
- **The fetch and scan pools use `errgroup.Group` with `SetLimit`, never
  `errgroup.WithContext`.** One repository's failure must not cancel the other
  ninety-nine. Per-item errors ride on the result; only the caller's context
  cancels.
- **Dirt belongs to a worktree, not a branch.** Only a branch checked out
  somewhere can carry a `Status`; a repository with linked worktrees has
  several at once. Never attribute working-tree changes to every branch.
- **`no-upstream` and `gone` are different states.** The first is an ordinary
  local branch — four of the ten repositories this was designed against are in
  it — and the second is a real problem. Six `SyncState` values exist for this
  reason, and `exhaustive` is on, so every switch over them needs a `default`.
- **Rebuild the tree, do not patch it.** Compaction is not stable as
  repositories stream in: `a/b/repo1` collapses to one row, and `a/c/repo2`
  arriving forces it to split. `Save`/`Restore` carry per-node state across,
  keyed by **absolute path** — never by the display label, which compaction
  changes.
- **The cursor is identified by row ID, not by index.** `cursorID` is
  authoritative and `cursor` is derived, so the cursor stays on the same
  repository when rows appear above it.
- **Nothing writes to stderr while the UI is up.** `runTUI` sets
  `log.SetOutput(io.Discard)`; a stray `log.Print` would paint over the
  alternate screen.
- **`resolveRoot` resolves symlinks.** `git rev-parse --show-toplevel` reports
  the real path, so a root that still contains a symlink is not a prefix of any
  repository path and every row renders as an absolute path. On macOS `/tmp`
  and `/var` are symlinks, so this bites immediately.
- **Regenerate docs after changing the command tree**: `make docs`.
  `make pipeline` fails if `docs/` is stale.
- **Run `make pipeline` before pushing.** It runs exactly what CI runs. The
  pre-push hook does it once `make hooks` has been run.
- No viper. The config is a small typed `config.yaml` handled by
  `internal/config`, and one rule — flag beats file beats default — which needs
  no framework. Pointer fields distinguish "unset" from "set to false".
