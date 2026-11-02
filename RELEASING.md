# Releasing orbit-cli

`orbit-cli` ships two Go binaries, `orbit` and `orbit-server`, for linux and darwin on amd64 and
arm64, plus the `ghcr.io/manovaspace/orbit-server` container image. A release ends with a `vX.Y.Z`
git tag, a GitHub release carrying the binaries and `checksums.txt`, and — when the GHCR credentials
are configured — a container image tagged with the version and `latest`.

## How a release happens

Write Conventional Commit messages and merge them into `main` through a pull request. On every push
to `main`, the `release-please` workflow opens or updates one bot release pull request titled
`chore(main): release X.Y.Z`. That pull request runs the ordinary `CI` workflow. When it is green and
reviewed, a maintainer merges it, and the same workflow creates the `vX.Y.Z` tag and the GitHub
release with the generated notes. Nobody bumps a version or pushes a tag by hand.

The release branch (`release-please--branches--main`) exists only while the pull request is open.
There is no long-lived release branch (ADR-044 D2).

## Order: notes first, binaries second

Merging the release pull request makes `release-please` create the tag and the GitHub release with
its generated notes. The tag push then runs `.github/workflows/release.yml`, which builds the four
`orbit`/`orbit-server` pairs, writes `checksums.txt`, attaches those files to the release that
already exists, and finally builds and pushes the `orbit-server` image.

The publish step handles both orders:

- If the GitHub release exists, it uploads `dist-merged/*` (the binaries and `checksums.txt`) with
  `gh release upload --clobber`, so the release-please notes are kept.
- If it does not exist, it falls back to `gh release create` with the existing title and generated
  checksum notes. A `v*` tag pushed by hand therefore still produces a complete release.

The tag is created with `RELEASE_TOKEN`, not the default `GITHUB_TOKEN`. That matters: events caused
by the default token do not start other workflows, so a tag created with it would never run
`release.yml` and the release would stay without binaries. Do not replace the token in
`.github/workflows/release-please.yml` with `secrets.GITHUB_TOKEN`.

## Conventional Commit types and what bumps

| Commit | Bump |
| --- | --- |
| `feat:` / `feature:` | MINOR |
| `fix:`, `perf:`, `refactor:`, `revert:` | PATCH |
| `!` after the type or scope (`feat!:`, `fix(api)!:`), or a `BREAKING CHANGE:` footer | MINOR while the version is `0.x`; MAJOR from `1.0.0` |
| `chore:`, `docs:`, `ci:`, `test:`, `style:`, `build:` | none |
| any type not listed above | none |

`refactor:` bumps a patch because `release-please-config.json` lists it as a visible changelog
section; the release-please defaults would hide it and release nothing. A release pull request
appears only when at least one merged commit has a visible section, so a docs-only or CI-only change
does not create one. Squash titles are the commit subject on `main`, so the pull request title must
also be a Conventional Commit.

`bump-minor-pre-major` is on, so a breaking change while the project is `0.x` bumps MINOR rather
than MAJOR. `bump-patch-for-minor-pre-major` is off, so `feat:` keeps bumping MINOR rather than
PATCH. When the project reaches `1.0.0`, breaking changes become MAJOR.

The latest tag was `v0.10.0`, so `.release-please-manifest.json` starts at `0.10.0`. The next
release pull request proposes the version the merged commits call for.

## How to read the release pull request

- **Title** — `chore(main): release X.Y.Z`.
- **Branch** — `release-please--branches--main`.
- **Label** — `autorelease: pending` until the release is tagged.
- **Files** — only `CHANGELOG.md` (a new version section) and `.release-please-manifest.json` (the
  new version). Any other changed file means the release pull request is wrong.
- **Body** — the commits and the notes for the proposed version. It updates as new commits land on
  `main`, and the proposed version grows with them.

Never edit the release pull request by hand except to resolve a genuine conflict in `CHANGELOG.md`.
Land the conflicting change on `main` and let the bot rebuild the pull request.

## Checks on the release pull request

`.github/workflows/ci.yml` runs on every pull request into `main`, including the release pull
request, and calls `manovaspace/ci@v1` with `stack: go`. It is the same gate as any other pull
request; there is no separate release-tier workflow in this repository.

Do not merge while this check is red or pending. `o pr merge` refuses in both cases (ADR-037).

## What merging does

The merge push runs `release-please` again. It creates the tag `vX.Y.Z` on `main` and publishes a
GitHub release whose notes come from the `CHANGELOG.md` entry, then relabels the pull request
`autorelease: tagged`. The tag push runs `release.yml`:

1. Builds `orbit` and `orbit-server` for linux/darwin amd64/arm64 with `VERSION`, `COMMIT` and the
   build date in the linker flags.
2. Writes `checksums.txt` over the binary names.
3. Attaches the binaries and `checksums.txt` to the GitHub release, or creates the release when a
   tag was pushed by hand.
4. Builds and pushes `ghcr.io/manovaspace/orbit-server` with the release version and `latest`, when
   `GHCR_TOKEN` is set.

No deploy runs from this repository.

## Rollback

Do not move or force-push a published tag. If a release is wrong, revert the change through a
`fix:` or `revert:` pull request and let the bot publish the next patch.

If a tag should never have existed and no consumer could have fetched it, the owner may delete the
GitHub release and the tag, then revert the release commit on `main` through a pull request. Deleting
a tag is destructive and needs the owner.

A pushed container image cannot be withdrawn from GHCR the same way. Publish a fixed patch and point
deployments at the new digest.

## Who approves

The bot never merges its own pull request. A maintainer reviews the release pull request and merges
it with `o pr merge`, which refuses while checks fail or are pending (ADR-037).

## Secrets used

Names only — never put a value in this file.

| Secret | Used by | Purpose |
| --- | --- | --- |
| `RELEASE_TOKEN` | `release-please` workflow | Personal access token so the release pull request triggers its own checks and the tag push triggers `release.yml` |
| `GH_READ_TOKEN` | CI | Read access to private `github.com/manovaspace` modules |
| `NPM_TOKEN` | CI | Read access to the `@orbit` npm scope |
| `GHCR_USERNAME` | `release.yml` container job | GHCR login user for the `orbit-server` image |
| `GHCR_TOKEN` | `release.yml` container job | GHCR push token; when empty the image build and push are skipped |

If `RELEASE_TOKEN` is empty, the `release-please` workflow prints `RELEASE_TOKEN not set, skipping`
and succeeds without opening a release pull request.
