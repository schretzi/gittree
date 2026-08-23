## gittree config init

Print a commented reference configuration

### Synopsis

init writes a fully commented configuration to stdout.

It is printed rather than written to disk so nothing is ever overwritten
behind your back:

    mkdir -p "$(dirname "$(gittree config path)")"
    gittree config init > "$(gittree config path)"

```
gittree config init [flags]
```

### Options

```
  -h, --help   help for init
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

