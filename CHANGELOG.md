# Changelog

## [0.23.1](https://github.com/manovaspace/orbit-cli/compare/v0.23.0...v0.23.1) (2026-11-28)


### Bug Fixes

* **install:** bump fallback version to v0.23.0 ([06723c1](https://github.com/manovaspace/orbit-cli/commit/06723c1417bf33d0ac8c664aa368fbf3895ea93f))
* **install:** bump fallback version to v0.23.0 ([713a0df](https://github.com/manovaspace/orbit-cli/commit/713a0df87277c75c61232ed2e07ea38bee7065d8))

## [0.23.0](https://github.com/manovaspace/orbit-cli/compare/v0.22.1...v0.23.0) (2026-11-25)


### ⚠ BREAKING CHANGES

* o onboard, o init, o dev tunnel and the orbit remove uninstall alias are gone. Use o setup, o doctor and orbit uninstall or orbit purge instead. orbit remove now stops selecting a project.

### Features

* remove o onboard and the dev tunnel; host gate allows setup on darwin ([88a9ed9](https://github.com/manovaspace/orbit-cli/commit/88a9ed90f1572a644811e138746163cb0f83ad71))
* **runner:** add env and interactive command seams ([76ce44f](https://github.com/manovaspace/orbit-cli/commit/76ce44f3e86557f44337feda71730145ab3eda13))
* **setup:** add identity and handbook parts ([9b6256a](https://github.com/manovaspace/orbit-cli/commit/9b6256a21bc494f4e9571fa8b3dc9fe5908e66d9))
* **setup:** add native services and o up/down/services ([6a46dee](https://github.com/manovaspace/orbit-cli/commit/6a46dee82fdca04c05775613eef4b1df8388d4bc))
* **setup:** add o setup, add, remove, projects, doctor check and init alias ([fa971f4](https://github.com/manovaspace/orbit-cli/commit/fa971f47a78a6195e7ab6200a33c20dddd12eb7e))
* **setup:** add package managers and the system part ([2d84057](https://github.com/manovaspace/orbit-cli/commit/2d8405717167bd92a3bc9d0b51d92c3f2ce7dcc7))
* **setup:** add project, machine, repo, root and package declarations ([a64ab56](https://github.com/manovaspace/orbit-cli/commit/a64ab56ad5df6b619402fff306cc8470abc7ce1b))
* **setup:** add the env part; schema from and for ([9879e74](https://github.com/manovaspace/orbit-cli/commit/9879e74e03352632ed65d35289bed5d51c45dadf))
* **setup:** add the prepare part ([12f074b](https://github.com/manovaspace/orbit-cli/commit/12f074bc007cff252d62de7b30a5609dfd6604ae))
* **setup:** add the repos part ([2acc4c0](https://github.com/manovaspace/orbit-cli/commit/2acc4c0e8ed9cedb19575b733b84a45277aa491d))
* **setup:** add the root part; audit reads root.yaml ([19af86c](https://github.com/manovaspace/orbit-cli/commit/19af86c1a07b85896e034ec9777ccf31c49fa4ac))
* **setup:** add the setup engine ([ae4cc83](https://github.com/manovaspace/orbit-cli/commit/ae4cc83633bec5165562335c1d7cfc7f73e5fdfa))
* **setup:** add the toolchains part and shell block ([b97f538](https://github.com/manovaspace/orbit-cli/commit/b97f538704202a432f9d7912661e00654f4d8aba))


### Bug Fixes

* **invite,docs:** update stale orbit onboard/init references and goldens ([1a7beb5](https://github.com/manovaspace/orbit-cli/commit/1a7beb54efdac1633c57aa99bfd0df690d898ec4))
* **setup:** --yes never prompts; rc names per shell; save flags before phase one ([bdf6db5](https://github.com/manovaspace/orbit-cli/commit/bdf6db5c39528ade1740349fbe75ad8577a0af0a))
* **setup:** bump apt lock timeout to 300s for initial unattended-upgrades ([cfbc248](https://github.com/manovaspace/orbit-cli/commit/cfbc2481f6335321c7eeeae3252842c81b1d3d51))
* **setup:** grant BYPASSRLS to local dev postgres owner roles and fix lint errors ([76a6748](https://github.com/manovaspace/orbit-cli/commit/76a6748f4b929371211556b9450b8156a0508f1d))
* **setup:** harden apt, GOPRIVATE, GOTOOLCHAIN, root guard, config repair and seed retry ([274e1db](https://github.com/manovaspace/orbit-cli/commit/274e1db39c14fed99865788fb2d2824d6a230936))
* **setup:** install unzip for bun; warn on odd package.json ([9ba009a](https://github.com/manovaspace/orbit-cli/commit/9ba009a3880d404530800ff7acec47484c56098a))
* **setup:** make the brew installer, PATH and apt bootstrap work ([647e084](https://github.com/manovaspace/orbit-cli/commit/647e08479867d8dabdb888a610372495aa6d7646))
* **setup:** never block setup on git identity; harden handbook clone ([d8e12fb](https://github.com/manovaspace/orbit-cli/commit/d8e12fb168cacf155c7227619d451ad9e195cbcc))
* **setup:** never overwrite declared files; cover root edge paths ([b123557](https://github.com/manovaspace/orbit-cli/commit/b12355753b8d82f45feed8d05545b9511fde0ae0))
* **setup:** remove orphaned onboard packages; drop retired hint; keep projects out of machine file until manifest exists ([ced0451](https://github.com/manovaspace/orbit-cli/commit/ced0451294019977e1f0b6143e421ed299299404))
* **setup:** report foreign Postgres before binaries; verify mailpit ownership ([417c4bd](https://github.com/manovaspace/orbit-cli/commit/417c4bd10014442fa30149910c9b9ee094fced1c))
* **setup:** wait for apt/dpkg lock with DPkg::Lock::Timeout=120 ([eab950d](https://github.com/manovaspace/orbit-cli/commit/eab950d2f13e31080c8c51fb5ba1b89998b4a42b))

## [0.22.1](https://github.com/manovaspace/orbit-cli/compare/v0.22.0...v0.22.1) (2026-11-23)


### Bug Fixes

* **workspace:** ignore the ~/.orbit cache as a workspace marker ([0924a52](https://github.com/manovaspace/orbit-cli/commit/0924a5200c4027629312b2adc0922552f91fb0e5))
* **workspace:** ignore the ~/.orbit cache as a workspace marker ([723db3a](https://github.com/manovaspace/orbit-cli/commit/723db3a8bcf365fbc48d8049df4f22cf40e552b1)), closes [#150](https://github.com/manovaspace/orbit-cli/issues/150)

## [0.22.0](https://github.com/manovaspace/orbit-cli/compare/v0.21.0...v0.22.0) (2026-11-23)


### Features

* release-aware merge gate for dev-stage CI ([aa3616b](https://github.com/manovaspace/orbit-cli/commit/aa3616bda44d23f48ec7cc0ac88f9b9fd615aadf))
* release-aware merge gate for dev-stage CI ([b428361](https://github.com/manovaspace/orbit-cli/commit/b4283619cdbf5020edc72f28aa44974d0cc579f5))


### Bug Fixes

* **pr:** require a passing secret scan on feature pull requests ([4f73ad2](https://github.com/manovaspace/orbit-cli/commit/4f73ad2517b9aa3286f782ce596bc43adf46666d))
* **pr:** require a passing secret scan on feature pull requests ([5a07f03](https://github.com/manovaspace/orbit-cli/commit/5a07f03e695ed031e9c8f7ca0b97f9a3d3046dd8)), closes [#147](https://github.com/manovaspace/orbit-cli/issues/147) [#148](https://github.com/manovaspace/orbit-cli/issues/148) [#149](https://github.com/manovaspace/orbit-cli/issues/149)

## [0.21.0](https://github.com/manovaspace/orbit-cli/compare/v0.20.0...v0.21.0) (2026-11-23)


### ⚠ BREAKING CHANGES

* **orbit-server:** orbit-server starts in production mode and needs a signing secret (--signing-secret, ORBIT_SIGNING_SECRET or an owner vault). Pass --dev or set ORBIT_SERVER_DEV=1 for the development defaults.

### Bug Fixes

* **deps:** drop the orbit-observability exclude; take orbit-notifications v0.5.2 ([#138](https://github.com/manovaspace/orbit-cli/issues/138)) ([6948df9](https://github.com/manovaspace/orbit-cli/commit/6948df90d793f9a0493b6a947739417738236fe9))
* **orbit-server:** require --dev for development defaults ([e4b98e5](https://github.com/manovaspace/orbit-cli/commit/e4b98e520c290a187dbc42c92bd3e4faffea7496))


### Code Refactoring

* **doctor,audit:** remove shell-output tool checks ([#130](https://github.com/manovaspace/orbit-cli/issues/130)) ([f0a66ff](https://github.com/manovaspace/orbit-cli/commit/f0a66ffe120c0fb2a920b7d42b246c46f75ea773))

## [0.20.0](https://github.com/manovaspace/orbit-cli/compare/v0.19.4...v0.20.0) (2026-11-21)


### Features

* **dev:** add orbit dev tunnel and dev routes from the port registry; fix stale dev endpoints ([#136](https://github.com/manovaspace/orbit-cli/issues/136)) ([d25dddb](https://github.com/manovaspace/orbit-cli/commit/d25dddbad7bd084bf6223dd61411391725cb8647))
* **host:** support macOS for developer commands; refuse server-only commands and doctor --fix there ([#135](https://github.com/manovaspace/orbit-cli/issues/135)) ([3bf6666](https://github.com/manovaspace/orbit-cli/commit/3bf66668e53435171d363a7298e493cb8746d43f))
* **port:** read the workspace port registry; add o port check and registry-aware list, allocate and doctor ([#133](https://github.com/manovaspace/orbit-cli/issues/133)) ([02bb88d](https://github.com/manovaspace/orbit-cli/commit/02bb88d61e6db148f50f8c31037933b0b595b5ad))

## [0.19.4](https://github.com/manovaspace/orbit-cli/compare/v0.19.3...v0.19.4) (2026-11-20)


### Bug Fixes

* **deps:** upgrade Go to 1.27.4 and patch vulnerable modules ([#124](https://github.com/manovaspace/orbit-cli/issues/124)) ([926e76b](https://github.com/manovaspace/orbit-cli/commit/926e76bf8c8b94e3c48feb40ded27807708d6e17))

## [0.19.3](https://github.com/manovaspace/orbit-cli/compare/v0.19.2...v0.19.3) (2026-11-08)


### Bug Fixes

* **self-update:** install exactly the target release and never skip checksums ([844805d](https://github.com/manovaspace/orbit-cli/commit/844805d9a07945079c99385820030172bb2e95c9))
* **self-update:** install exactly the target release and never skip checksums ([0714ab5](https://github.com/manovaspace/orbit-cli/commit/0714ab578d3b1422b86a4e529a85e941cbc23953)), closes [#109](https://github.com/manovaspace/orbit-cli/issues/109)

## [0.19.2](https://github.com/manovaspace/orbit-cli/compare/v0.19.1...v0.19.2) (2026-11-08)


### Bug Fixes

* **deps:** update github.com/aws/aws-sdk-go-v2/config to v1.33.14 on current main ([31ddaeb](https://github.com/manovaspace/orbit-cli/commit/31ddaeb754e6b014d9dcb6fb79a46f6ba1176447))

## [0.19.1](https://github.com/manovaspace/orbit-cli/compare/v0.19.0...v0.19.1) (2026-11-08)


### Bug Fixes

* **ports:** block 2 belongs to falak, not fryto ([5c095f8](https://github.com/manovaspace/orbit-cli/commit/5c095f82b00c24cbdb1d5840ecaee76a50c9384f))
* **ports:** block 2 belongs to falak, not fryto ([3e20a8e](https://github.com/manovaspace/orbit-cli/commit/3e20a8e21b20e9ec968f1ce894cc2c90b3c350a2))

## [0.19.0](https://github.com/manovaspace/orbit-cli/compare/v0.18.2...v0.19.0) (2026-11-06)


### Features

* **render:** finish fmt.Fprint migration batches 3-5 and add --json contract test ([0e155fe](https://github.com/manovaspace/orbit-cli/commit/0e155fed90fdaceeb906e5ea017e7a8a32e9917e))
* **render:** finish fmt.Fprint migration batches 3-5 and add --json contract test (closes [#85](https://github.com/manovaspace/orbit-cli/issues/85)) ([925a144](https://github.com/manovaspace/orbit-cli/commit/925a144a7aa3b7516ca4e131cce0dc6d4b97356c))
* **tui:** migrate bubbletea, bubbles, and lipgloss to v2 (closes [#78](https://github.com/manovaspace/orbit-cli/issues/78)) ([024f71e](https://github.com/manovaspace/orbit-cli/commit/024f71e8899226cb0d026af35e104fc147c3cb93))
* **tui:** migrate charm dependencies to v2 ([1eda579](https://github.com/manovaspace/orbit-cli/commit/1eda5790687aba995c07c09834e5a4da38df49d3))

## [0.18.2](https://github.com/manovaspace/orbit-cli/compare/v0.18.1...v0.18.2) (2026-11-05)


### Bug Fixes

* **ci:** report the org runner pool instead of a local systemctl unit ([0d2c258](https://github.com/manovaspace/orbit-cli/commit/0d2c258594a4f2b3c5a5604051d149e408f1fb3b))
* **ci:** report the org runner pool instead of a local systemctl unit ([58cef55](https://github.com/manovaspace/orbit-cli/commit/58cef55eea087a0b8190f2acb440cf3a3a7795bc))

## [0.18.1](https://github.com/manovaspace/orbit-cli/compare/v0.18.0...v0.18.1) (2026-11-05)


### Bug Fixes

* **audit:** resolve the newest node_modules copy ([f288b4d](https://github.com/manovaspace/orbit-cli/commit/f288b4d1d9772e4bef028f94da525eacca26850f))
* **audit:** resolve the newest node_modules copy ([333b458](https://github.com/manovaspace/orbit-cli/commit/333b4580578f608ede528b155e0f5be31ecfa861))

## [0.18.0](https://github.com/manovaspace/orbit-cli/compare/v0.17.0...v0.18.0) (2026-11-05)


### Features

* **ports:** move orbit-server into the orbit block ([554d997](https://github.com/manovaspace/orbit-cli/commit/554d997f4ea683078cd7a2c43c13445695cee56b))
* **ports:** move orbit-server into the orbit block ([ce3ee52](https://github.com/manovaspace/orbit-cli/commit/ce3ee52c9953bed8a39c76f5d6778b07c62fabfb))

## [0.17.0](https://github.com/manovaspace/orbit-cli/compare/v0.16.0...v0.17.0) (2026-11-05)


### Features

* **dev:** use the orbit-block dev tooling ports ([9b63bcb](https://github.com/manovaspace/orbit-cli/commit/9b63bcb03589367c08954a397513d3fabbb51cde))
* **dev:** use the orbit-block dev tooling ports ([ecedf9b](https://github.com/manovaspace/orbit-cli/commit/ecedf9b1b5f254c7acb3ec4971c676ef7c4d819b))

## [0.16.0](https://github.com/manovaspace/orbit-cli/compare/v0.15.0...v0.16.0) (2026-11-03)


### Features

* **pr:** flat status table with URL detail line ([36deedc](https://github.com/manovaspace/orbit-cli/commit/36deedc388272912b838b01931998cee49d5d428))
* **pr:** flat status table with URL detail line ([fc75fc5](https://github.com/manovaspace/orbit-cli/commit/fc75fc595931df539597e0f71bc4abe92905cba3))

## [0.15.0](https://github.com/manovaspace/orbit-cli/compare/v0.14.0...v0.15.0) (2026-11-03)


### Features

* **cli:** render inspection commands through pkg/render (ADR-047 phase 3, batch 1) ([98efa50](https://github.com/manovaspace/orbit-cli/commit/98efa50c6f3cf05f8123ac9ca2ee0205e2b46837))
* **cli:** render inspection commands through pkg/render (ADR-047 phase 3, batch 1) ([9c43cb9](https://github.com/manovaspace/orbit-cli/commit/9c43cb958391e16e9086aa34011438b80bf04219))

## [0.14.0](https://github.com/manovaspace/orbit-cli/compare/v0.13.0...v0.14.0) (2026-11-03)


### Features

* **cli:** one-line errors, JSON error envelopes and version --json (ADR-047 phase 2) ([6acd1b5](https://github.com/manovaspace/orbit-cli/commit/6acd1b51637223783789a69b146683b8d007dd5f))
* **cli:** one-line errors, JSON error envelopes and version --json (ADR-047 phase 2) ([5b7f00f](https://github.com/manovaspace/orbit-cli/commit/5b7f00f2526dde33cafa3f0d2ee489b2b3a16ec4))

## [0.13.0](https://github.com/manovaspace/orbit-cli/compare/v0.12.0...v0.13.0) (2026-11-02)


### Features

* **render:** add pkg/render shared output layer (ADR-046 phase 1) ([f3fdeaa](https://github.com/manovaspace/orbit-cli/commit/f3fdeaa7612c5d68a96785267d5500ad1f5bc4a9))
* **render:** add pkg/render shared output layer (ADR-046 phase 1) ([f35d257](https://github.com/manovaspace/orbit-cli/commit/f35d2579166674a21b1ef024c07a0f003373100f))

## [0.12.0](https://github.com/manovaspace/orbit-cli/compare/v0.11.0...v0.12.0) (2026-11-02)


### Features

* **pr:** list open PRs across workspace repos by name pattern ([a8a2341](https://github.com/manovaspace/orbit-cli/commit/a8a2341aff8132cff1907a952e2570653ef6ee29))
* **pr:** list open PRs across workspace repos by name pattern ([f244cd2](https://github.com/manovaspace/orbit-cli/commit/f244cd26178e1270de7e11910d643aa6ab77c701))

## [0.11.0](https://github.com/manovaspace/orbit-cli/compare/v0.10.0...v0.11.0) (2026-11-02)


### Features

* **audit:** accept named UNVERIFIED items and fix false positives ([#46](https://github.com/manovaspace/orbit-cli/issues/46)) ([0f1e124](https://github.com/manovaspace/orbit-cli/commit/0f1e124fe8ccfe94db5cf79a2051cd47b3a451eb))
* **audit:** flag retired names in active docs and keep the audit read-only ([#48](https://github.com/manovaspace/orbit-cli/issues/48)) ([4875bff](https://github.com/manovaspace/orbit-cli/commit/4875bff7676a095a093eb98b5775246ea4f3ae53))
