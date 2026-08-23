## gittree

Browse the state of every git repository under a directory

### Synopsis

gittree finds every git repository beneath a directory and shows them as
one scrollable tree: each repository's local branches, how each branch stands
against its upstream, and what is uncommitted in the working tree.

It never changes a repository. Pressing enter on one hands the terminal to
lazygit (or to your shell) so you can act on it there, and rescans the row
when you come back.

With no path, gittree starts from the current directory. When stdout is not a
terminal it prints the same data as "gittree list" instead of drawing a UI, so
piping and redirecting work.

```
gittree [flags] [path]
```

### Examples

```
  # browse everything under the current directory
  gittree

  # browse a specific tree
  gittree ~/Workspace

  # the same data, scriptable
  gittree list --dirty ~/Workspace
```

### Options

```
      --concurrency int          how many repositories to fetch at once (default 8)
      --config string            path to config.yaml (default: XDG config directory)
      --depth int                how many directory levels below the root to search (0 for unlimited) (default 8)
      --fetch-timeout duration   give up on a single repository's fetch after this long (default 20s)
  -h, --help                     help for gittree
      --ignore stringArray       directory name to skip (repeatable)
      --log-file string          write debug logs to this file
      --nested                   descend into repositories to find repositories inside them
      --no-color                 disable colour output
      --no-fetch                 do not fetch in the background on startup
      --no-ignore-defaults       do not skip the built-in list of directories such as node_modules
      --scan-concurrency int     how many repositories to scan at once (0 for one per CPU)
  -v, --verbose                  report skipped and unreadable directories
```

### SEE ALSO

* [gittree config](gittree_config.md)	 - Show or create the configuration file
* [gittree list](gittree_list.md)	 - Print the state of every repository, one per line
* [gittree version](gittree_version.md)	 - Print version, build and runtime information

