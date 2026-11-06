## orbit release

Inspect and promote release pull requests and demo branches

### Synopsis

Watch the release-tier check of the bot release pull request, promote main to demo, and merge the release pull request. There is no long-lived release branch: changesets and release-please open one pull request into main per repository.

### Options

```
  -h, --help              help for release
      --manifest string   Path to workspace.yaml
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit release check](orbit_release_check.md)	 - Watch the release-tier check, or run the release tier against the local dev services
* [orbit release demo](orbit_release_demo.md)	 - Promote main to demo through a pull request
* [orbit release prod](orbit_release_prod.md)	 - Merge the open release pull request through the merge gate
* [orbit release status](orbit_release_status.md)	 - Report the release pull request, demo branch and last publish or deploy run

