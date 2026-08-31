## orbit assets

Sync gitignored media with private Cloudflare R2

### Synopsis

Pull, push, add, and status for files listed in orbit-assets.yaml. Git stores the index; R2 stores bytes.

### Options

```
  -h, --help   help for assets
```

### Options inherited from parent commands

```
      --config string   Custom path to Orbit CLI configuration file
```

### SEE ALSO

* [orbit](orbit.md)	 - Orbit developer platform and workspace orchestrator
* [orbit assets add](orbit_assets_add.md)	 - Hash, upload, and gitignore a file; update orbit-assets.yaml
* [orbit assets pull](orbit_assets_pull.md)	 - Download missing or outdated assets for workspace repos
* [orbit assets push](orbit_assets_push.md)	 - Upload index objects that are missing from R2
* [orbit assets status](orbit_assets_status.md)	 - Show missing or mismatched local assets

