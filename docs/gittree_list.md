## gittree list

Print the state of every repository, one per line

### Synopsis

list prints the same information the UI shows, as plain text.

It is what gittree falls back to when stdout is not a terminal, so piping and
redirecting a bare "gittree" produces this too.

```
gittree list [flags] [path]
```

### Examples

```
  # every repository below the current directory
  gittree list

  # only the ones with uncommitted changes
  gittree list --dirty

  # every branch, not just the checked-out one
  gittree list --branches ~/Workspace

  # for scripting
  gittree list --json | jq '.[] | select(.sync == "behind")'
```

### Options

```
  -b, --branches               print every local branch, not just the checked-out one
      --depth int              how many directory levels below the root to search (0 for unlimited) (default 8)
  -d, --dirty                  print only repositories with uncommitted changes
  -h, --help                   help for list
      --ignore stringArray     directory name to skip (repeatable)
      --json                   print JSON instead of a table
      --nested                 descend into repositories to find repositories inside them
      --no-ignore-defaults     do not skip the built-in list of directories such as node_modules
      --scan-concurrency int   how many repositories to scan at once (0 for one per CPU)
```

### Options inherited from parent commands

```
      --config string     path to config.yaml (default: XDG config directory)
      --log-file string   write debug logs to this file
      --no-color          disable colour output
  -v, --verbose           report skipped and unreadable directories
```

### SEE ALSO

* [gittree](gittree.md)	 - Browse the state of every git repository under a directory

