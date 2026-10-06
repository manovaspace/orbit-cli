## orbit config list

List all resolved configuration entries and their sources

### Synopsis

Displays all active configuration parameters across defaults, configuration file, environment variables, and flags.

```
orbit config list [flags]
```

### Options

```
  -f, --format string   Output format: table, json, or yaml (default "table")
  -h, --help            help for list
      --limit int       maximum rows per page (0 = all)
      --page int        page number to display (default 1)
```

### Options inherited from parent commands

```
      --config string   Custom path to configuration file
```

### SEE ALSO

* [orbit config](orbit_config.md)	 - Manage Orbit CLI configuration
