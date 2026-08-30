# gittree — the state of every repository, in one tree

`gittree` finds every git repository beneath a directory and shows them as one
scrollable tree: each repository's local branches, how each branch stands
against its upstream, and what is uncommitted in the working tree. It is the
answer to "what is the state of all of this?" without `cd`-ing around running
`git status` by hand.

It never changes a repository. Pressing enter on one suspends the UI and hands
the whole terminal to [lazygit](https://github.com/jesseduffield/lazygit) — or
to your shell — so you can act on it with a tool built for that, and the row
rescans when you come back.

gittree is MIT licensed.

```
/Users/you/Workspace                                                                           5 repos
repository                      branch  sync  upstream     local  fetched
▾ ▪ Schretzi
  ▸ ◆ 3rdParty/dotfiles-latest  main    ✓     origin/main  ~2     never
  ▸ ◆ KerberosKeepAlive         main    ↑2    origin/main  ~1     3h
  ▸ ◆ MacbookSetup              main    —                  ~4     never
  ▸ ◆ macswitcher               main    ✓     origin/main  ?1     2h
  ▸ ◆ mySetup                   main    ↑1↓3  origin/main  ~2 ?2  2h (fetch failed: ssh key rejected)
```

## Install

```sh
brew install schretzi/tap/gittree
go install github.com/schretzi/gittree/cmd/gittree@latest
```

Or take a binary from the [releases](https://github.com/schretzi/gittree/releases).

`git` 2.29 or newer is required. `lazygit` is optional — without it, enter
opens your shell in the repository instead.

## Usage

```sh
gittree                      # everything below the current directory
gittree ~/Workspace          # somewhere else
gittree --no-fetch           # skip the startup fetch, stay offline
gittree --depth 3            # search less deeply
gittree --nested             # look inside repositories too (submodules)

gittree list                 # the same data as plain text
gittree list --dirty         # only repositories with uncommitted changes
gittree list --branches      # every local branch, not just the checked-out one
gittree list --json | jq '.[] | select(.sync == "behind")'
```

When stdout is not a terminal, a bare `gittree` prints the `list` output
instead of drawing a UI, so `gittree | grep` and `gittree > out.txt` behave.

### Keys

| | |
|---|---|
| `↑`/`k`, `↓`/`j` | move |
| `g`, `G` | first row, last row |
| `^u`, `^d` | half a page |
| `→`/`l`, `←`/`h`, `space` | expand, collapse, toggle |
| `E`, `C` | expand all, collapse all |
| `enter` | open lazygit on a repository; expand a directory |
| `o`, `s` | open the tool, open a shell |
| `r`, `R` | rescan this repository, rescan everything |
| `f`, `F` | fetch this repository, fetch everything |
| `p` | pull this repository |
| `d` | show only repositories with uncommitted changes |
| `?`, `q` | help, quit |

`enter` means two things because a directory has no repository to open. Every
expansion key works on every row, so nothing is reachable only through `enter`.

### What the columns mean

The columns are labelled above the tree, and `?` explains them from inside the
program, in whichever glyph set your terminal is using.

The sync column compares a branch to its upstream:

| | |
|---|---|
| `✓` | in sync |
| `↑2` | 2 commits ahead |
| `↓3` | 3 commits behind |
| `↑1↓3` | diverged |
| `—` | no upstream configured |
| `gone` | the upstream was configured and has since been deleted |

`—` and `gone` are different states and are worth keeping apart: the first is
an ordinary local branch, the second is a branch whose remote has vanished
underneath it.

Sync is local refs against remote-tracking refs, so it is only as current as
the last fetch — never against the remote as it is right now. When a fetch
fails, remote-tracking refs are exactly what did not get updated, so the sync
state is greyed out: the value stays, because it is still the best answer
available, but it stops claiming it was just verified.

The dirty column counts `+` staged, `~` modified, `?` untracked and `!`
conflicted. It appears on the repository row and on the checked-out branch,
because uncommitted changes belong to a working tree rather than to a branch —
only a branch that is checked out somewhere can have any. A repository with
linked worktrees has several such branches at once, and each carries its own.

The last column is how long ago a `git fetch` last ran in the repository, which
is also how current the sync column is — ahead/behind is measured against the
last fetch, not against the remote right now.

| | |
|---|---|
| `3h` | fetched three hours ago |
| `never` | no fetch has ever run here: a fresh clone, or a repository with no remote |
| `3h (fetch failed: ssh key rejected)` | gittree's own last fetch of this repository failed, and why; `3h` is when one last worked |

`never` is common and is not an error. A red `(fetch failed: …)` is, and it is
the one place a failed background fetch is reported — on a tree of any size an
unreachable remote must not produce a wall of messages. The reason is git's own
complaint boiled down to a few words: `ssh key rejected`, `no stored
credentials`, `host not found`, `host key not trusted`, `repository not found`,
`timed out` and so on. Anything gittree does not recognise is passed through as
git printed it.

The reason is the only thing on a row sized to a sentence, so it is also the
first thing to give width back: on a narrow window it truncates before any
column is dropped.

## Fetching

By default gittree fetches every repository in the background at startup, so
ahead/behind reflects the remote rather than whenever you last fetched by
hand. Rows update as results land, and `--no-fetch` turns it off.

The fetch is built so it can never hang the UI:

- Every prompt is disabled — `GIT_TERMINAL_PROMPT=0`, no askpass, ssh in
  `BatchMode`. A repository that would ask for a password fails immediately
  instead of waiting behind the alternate screen for input you cannot see.
- Each fetch has its own timeout, and the whole process group is killed when
  it expires. Killing only `git` is not enough: the `ssh` it spawned inherits
  the pipes and would keep the call blocked well past the timeout.
- One repository's failure never affects another. Failures are marked on their
  own row with `!`; nothing is printed over the UI.

`--prune` is on by default. Without it a deleted remote branch leaves its
tracking ref behind forever, so `gone` would never appear and gittree would
report a branch whose upstream vanished as being in sync. It only touches
`refs/remotes`; local branches and tags are never affected.

gittree also passes `--no-optional-locks` on every read, so it never takes
`index.lock` and never fights a shell you have open in the same repository.

## Configure

The config file is optional. Write one with:

```sh
mkdir -p "$(dirname "$(gittree config path)")"
gittree config init > "$(gittree config path)"
```

`gittree config init` prints a fully commented file showing every default;
delete the lines you do not want to change. Flags always win over the file.

## Build from source

```sh
git clone https://github.com/schretzi/gittree
cd gittree
make build
```

## Test

```sh
make test        # go test ./... -race -cover
make pipeline    # exactly what CI runs
```

Tests that need a real `git` build their repositories in a temp directory and
skip when it is missing or under `-short`. Everything else — path compaction,
the git output parsers, and the whole UI — runs without a terminal or a git
binary, because `Update` is a plain function of `(model, message)`.

## Development

```sh
lefthook install   # or: make hooks
make pipeline      # run before pushing; the pre-push hook does this for you
make docs          # regenerate docs/ after changing the command tree
```

## Releasing

Tag the commit, push the tag, then run the "Release" workflow manually from
the Actions tab and pick that tag. There is deliberately no on-tag-push
trigger: pushing a tag should not be able to publish a release by accident.

## AI usage

gittree was built with Claude Code. The architecture was decided jointly,
after checking how git, lazygit and Bubble Tea actually behave rather than
assuming — the three-process scan, the six sync states, and the process-group
kill in the fetch path all came out of testing the real thing and finding the
naive version wrong. The code was written by AI against a human-approved plan
and verified end to end against real repositories, not assumed to work. Treat
commit authorship as human-directed, AI-assisted.
