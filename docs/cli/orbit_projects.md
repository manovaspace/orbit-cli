## orbit projects

List the workspace projects, whether you chose them and whether their repos are cloned

### Synopsis

State is cloned when every repository of the project has a .git/HEAD on disk, not cloned when none
does, and partial otherwise. Nothing is fetched from the network.

```
orbit projects [flags]
```

### Options

```
  -h, --help   help for projects
      --json   Print the result as a JSON envelope
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator

