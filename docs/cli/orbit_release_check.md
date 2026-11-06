## orbit release check

Watch the release-tier check, or run the release tier against the local dev services

### Synopsis

Without --local, polls the open release pull request's checks until two consecutive polls report the same complete set. Exit 0 means pass, 1 means failure, 2 means no release pull request or no checks. With --local, creates a throwaway database in the existing dev Postgres, runs the repository's release tier against the dev Postgres, Redis and NATS, and drops the database in every case. It starts no containers.

```
orbit release check [repo] [flags]
```

### Options

```
  -h, --help                    help for check
      --json                    Print the result as a JSON envelope
      --local                   Run the release tier against the existing dev Postgres, Redis and NATS
      --wait-timeout duration   Maximum time to wait for release checks (default 20m0s)
```

### Options inherited from parent commands

```
      --config string     Custom path to Orbit CLI configuration file
      --manifest string   Path to workspace.yaml
```

### SEE ALSO

* [orbit release](orbit_release.md)	 - Inspect and promote release pull requests and demo branches

