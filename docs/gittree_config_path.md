## gittree config path

Print where the configuration file is, or would be

### Synopsis

path prints the config file gittree is using.

When no file exists anywhere, it prints where one would be looked for first,
so the output is always a usable target for redirection.

```
gittree config path [flags]
```

### Options

```
  -h, --help   help for path
```

### Options inherited from parent commands

```
      --config string     path to config.yaml (default: XDG config directory)
      --log-file string   write debug logs to this file
      --no-color          disable colour output
  -v, --verbose           report skipped and unreadable directories
```

### SEE ALSO

* [gittree config](gittree_config.md)	 - Show or create the configuration file

