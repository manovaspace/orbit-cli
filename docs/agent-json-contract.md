# Agent JSON contract

Machine-readable output for coding agents and scripts. This describes what `orbit` emits today. Agents assert on these JSON keys and exit codes, never on table text, column widths or colors.

Source of truth: `pkg/output/output.go` (envelope), `pkg/clierr/clierr.go` (exit and error codes), `cmd/orbit/main.go` (`run`, failure routing).

## Flags

| Flag | Where | Output |
| --- | --- | --- |
| `--json` | Every command except `completion` | Result wrapped in the envelope below, on stdout |
| `--format json` (`-f json`) | `admin status`, `invite list`, `config show`, `config list` only | `admin status` and `invite list`: same envelope as `--json`. `config show` and `config list`: bare JSON payload, no envelope |

Rules that follow from the code:

- `--json` on a failing command writes one failure envelope on stdout. The root `run` writes it only if the command did not already write one, so a command never produces two envelopes.
- A `--json` flag that appears after a literal `--` is ignored for failure routing.
- With `--format json` on `config show`/`config list`, a failure writes no envelope. The error goes to stderr as text, and the exit code still follows the table below.
- Text output (no JSON flag) writes failures as `Error: ...` plus an optional `hint:` line on stderr.

## Envelope

Every `--json` result is one line of JSON followed by a newline.

Success:

```json
{"schemaVersion":1,"ok":true,"data":{...}}
```

Failure:

```json
{"schemaVersion":1,"ok":false,"error":{"code":"failed","message":"sync completed with 1 error(s)"}}
```

Fields:

| Field | Type | Presence |
| --- | --- | --- |
| `schemaVersion` | integer | always; currently `1` |
| `ok` | boolean | always |
| `data` | any JSON value (object, array or scalar) | success only, omitted when the payload is nil; array payloads are never null (for example `invite list` returns `[]`) |
| `error.code` | string | failure only; one of the error codes below |
| `error.message` | string | failure only |
| `error.hint` | string | failure only; omitted when empty |

`data` is never present together with `error`.

## Error codes and exit codes

| Exit | `error.code` | Meaning |
| --- | --- | --- |
| 0 | (none, `ok: true`) | success |
| 1 | `failed` | operation ran and failed, or produced findings; any error without a clierr code also maps here |
| 2 | `unverified` | a check could not be performed |
| 3 | `usage` | invalid invocation, including cobra flag and argument errors |
| 4 | `auth` | missing or rejected credentials |
| 5 | `network` | transport failure or unreachable service |
| 6 | `tool` | an external tool failed or is missing |

## Examples from the golden files

`cmd/orbit/testdata/json/` pins real envelopes. For example `sync_failure.golden`:

```json
{"schemaVersion":1,"ok":false,"error":{"code":"failed","message":"sync completed with 1 error(s)"}}
```

## Agent guidance

1. Pass `--json` (or `--format json` where listed above) and parse stdout as JSON.
2. Check `schemaVersion` and `ok` first. Read `data` only when `ok` is true and `error` only when it is false.
3. Branch on `error.code` or the exit code. Do not match on `error.message` text.
4. Ignore keys you do not use. Payload keys inside `data` are per command.

## Stability

- `schemaVersion` is `1`. `pkg/output` bumps it only for an incompatible change to the envelope shape.
- The envelope fields, error codes and exit codes above are the contract. Renaming or removing one is an incompatible change.
- Per-command `data` payloads have no separate version. Their keys are pinned by golden files in `cmd/orbit/testdata/json/` and by tests such as `TestBatch1JSONStability`, `TestIdentityJSONFailureEnvelope` and `TestConfigShowAndInit`. Regenerate goldens only for intended changes with `go test ./cmd/orbit ./pkg/render -update-render`.

## Differences from the 2026-09 spec

The 2026-09 testing spec (section 3.1) said agents run commands with `--format json` (or `--json`) and that the two are interchangeable. The code differs:

1. `--json` is the envelope flag. `--format json` exists on only four commands. On `config show` and `config list` it prints a bare payload, not an envelope, and failures are not wrapped.
2. The spec did not define the envelope, failure routing or exit codes. They are defined here as implemented in `pkg/output`, `pkg/clierr` and `cmd/orbit/main.go`.
3. Fixed: `config show --format json` and `config show --json` no longer print plaintext values for `custom.*` keys that look like credentials (`pass`, `secret`, `token`, `key`, `auth`). They print `********`, as the spec requires (Tier-1 invariant 5 in `docs/testing/tier1-invariants.md`). `config list` and `config get` mask the same keys; `config get --raw` is the only plaintext path. The JSON contract itself is unchanged.
