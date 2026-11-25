## orbit add

Select projects and set up their repositories

### Synopsis

Changes the chosen projects in machine.yaml and applies the setup.
Removing a project never deletes a directory: repositories that are cloned but no longer
selected are listed, and you remove them yourself.

```
orbit add <project>... [flags]
```

### Options

```
  -h, --help   help for add
      --json   Print the result as a JSON envelope
      --yes    Apply without asking to confirm
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

