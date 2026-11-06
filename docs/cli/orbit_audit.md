## orbit audit

Audit workspace versions, dependencies, agents and delivery

### Synopsis

Report drift between the workspace and manovaspace/ci/ci/versions.env, resolved dependency versions, kit exports shadowed by local code, agent configuration, repositories without tests, upstream toolchain lag and merge-gate bypasses. The audit is read-only: it never edits a repository and reads the network only with --remote on the checks that support it. Exit codes: 0 no ERROR findings, 1 at least one ERROR, 2 something could not be verified.

```
orbit audit [flags]
```

### Options

```
  -h, --help     help for audit
      --json     Print the report as a JSON envelope
      --remote   Also run the network checks (registry, OSV, releases, merges, Actions billing)
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit audit agents](orbit_audit_agents.md)	 - Check handbook gates, editor configuration and agent runtimes
* [orbit audit deps](orbit_audit_deps.md)	 - Report resolved dependency versions, forbidden ranges and shadowed exports
* [orbit audit merges](orbit_audit_merges.md)	 - Report merged pull requests that bypassed the merge gate
* [orbit audit tests](orbit_audit_tests.md)	 - Report repositories with no tests and GitHub Actions usage
* [orbit audit upstream](orbit_audit_upstream.md)	 - Report OSV advisories and toolchain versions behind upstream
* [orbit audit usage](orbit_audit_usage.md)	 - Verify kit imports against the installed package exports
* [orbit audit versions](orbit_audit_versions.md)	 - Compare tool versions with manovaspace/ci/ci/versions.env

