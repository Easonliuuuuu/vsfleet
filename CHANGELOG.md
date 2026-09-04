# Changelog

## [0.3.0](https://github.com/Easonliuuuuu/vsfleet/compare/v0.2.0...v0.3.0) (2026-09-04)


### Features

* **assessment:** add deterministic RVTools exports ([ba3004d](https://github.com/Easonliuuuuu/vsfleet/commit/ba3004ddf230d3aa4a9bbf438d46409c62d4387b))
* **assessment:** add estate trends and ledger maintenance ([a14a814](https://github.com/Easonliuuuuu/vsfleet/commit/a14a81431775c9d237f04aa3050c0c1921369689))
* **assessment:** add labeled VM timelines and drift policies ([11cddf6](https://github.com/Easonliuuuuu/vsfleet/commit/11cddf611a4c102581851ed6fe92bd054b27e500))
* **assessment:** add persistent VM drift history and TUI changes ([e31382c](https://github.com/Easonliuuuuu/vsfleet/commit/e31382c704b8ded2810bc9cc86d659f555b77935))
* **report:** add per-VM disk and network inventory ([5c977fe](https://github.com/Easonliuuuuu/vsfleet/commit/5c977fea3186cdb3449226293c8a1e923fd16414))
* **skills:** add show-me visual skill and enhance git-commit with visual summaries ([7b92f4b](https://github.com/Easonliuuuuu/vsfleet/commit/7b92f4b7413c86ed73eee7081bb93633d8968d27))
* **tui:** prioritize the visible kind and fetch the rest concurrently ([5fb7a24](https://github.com/Easonliuuuuu/vsfleet/commit/5fb7a247095d237dcf8448ba46c4dd98633e98b9))
* **tui:** re-read inventory in the background so the table stays current ([eb7ebe9](https://github.com/Easonliuuuuu/vsfleet/commit/eb7ebe913cb7d2e98a52a88dd69dedb294e499de))
* **tui:** tier the background refresh by what is on screen ([00953c3](https://github.com/Easonliuuuuu/vsfleet/commit/00953c35c300a47f7fbb60dbb75dcc04f5c0223f))
* **vsphere:** add read-only vApp inventory support ([7c6249c](https://github.com/Easonliuuuuu/vsfleet/commit/7c6249c4d8f593e04cc5d33aeed7caaafeb02efb))
* **vsphere:** scope inventory retrieval to the configured datacenter ([46a4f7d](https://github.com/Easonliuuuuu/vsfleet/commit/46a4f7dceaae805b51b3cffbea7fb80d72f9c6f9))


### Bug Fixes

* **assessment:** remove unused timestamp helper ([1c225fa](https://github.com/Easonliuuuuu/vsfleet/commit/1c225fae1aae856506e42ba63b6e955f71945eac))
* **cli:** close export test history on windows ([a6dba87](https://github.com/Easonliuuuuu/vsfleet/commit/a6dba87382631edd842c0102c4b6a994e66f2b00))
* **contextops.go:** fall back to a prompt credential when the keyring is unavailable ([8c228cc](https://github.com/Easonliuuuuu/vsfleet/commit/8c228ccdebaf1502bbfb003b824597f02ad57df0))
* **session:** bound inventory enumeration by --timeout, not just connecting ([5564a74](https://github.com/Easonliuuuuu/vsfleet/commit/5564a744dcf57a13a4d57cde54100860c5dc7c6e))
* **tui:** lazily authenticate context panes ([391c1e6](https://github.com/Easonliuuuuu/vsfleet/commit/391c1e6a3fb5a09eac455351752aeac8105ac24f))
* **tui:** prioritize controls in focused search inputs ([2ef2733](https://github.com/Easonliuuuuu/vsfleet/commit/2ef2733fa2f9a7baa13d949cf9f639b229f55a54))
* **tui:** require a terminal before launching the interface ([7b70fbe](https://github.com/Easonliuuuuu/vsfleet/commit/7b70fbe149485e40734b01677de8733a6dc02398))
* **tui:** resolve the selected context's credentials before Bubble Tea and stop prefetching offscreen contexts ([7d18e78](https://github.com/Easonliuuuuu/vsfleet/commit/7d18e78733c54fd8e121ef0142f66194e10ec703))
* **tui:** stop background credential prompts from racing Bubble Tea for stdin ([71eafb0](https://github.com/Easonliuuuuu/vsfleet/commit/71eafb0d1df0f5705181d4d9ed47ebe25681edc1))
* **tui:** track inventory freshness per resource kind ([bf8cd25](https://github.com/Easonliuuuuu/vsfleet/commit/bf8cd25771d081ebb2c69cd239de8b5a58944c86))
* **ui.go:** keep pflag an indirect dependency ([eeb1363](https://github.com/Easonliuuuuu/vsfleet/commit/eeb1363e3a600c2d3f62d3df05386c6bc290a7a1))

## [0.2.0](https://github.com/Easonliuuuuu/vsfleet/compare/v0.1.0...v0.2.0) (2026-09-03)


### ⚠ BREAKING CHANGES

* **tui:** TUI keybindings changed. tab now opens the estate-wide search instead of switching panes, and n/e/x moved to the contexts screen behind c. Resource kinds gained 1-7 alongside the existing h/l cycling.

### Features

* **tui:** flatten the browse screen and add estate-wide search ([42cb93b](https://github.com/Easonliuuuuu/vsfleet/commit/42cb93b0bcd5a641177dd518ab618d16ea60fa8c))


### Bug Fixes

* **model.go:** clear the load note when the scope changes ([f89f93f](https://github.com/Easonliuuuuu/vsfleet/commit/f89f93f035b0a9e1e2ac4b6798f2a2eb81001ac9))
* **session:** invalidate a context's session and cache when it is edited ([b50762e](https://github.com/Easonliuuuuu/vsfleet/commit/b50762e03c13f72941fbc1f33de4531fe09c76b7))

## [0.1.0](https://github.com/Easonliuuuuu/vsfleet/compare/v0.0.1...v0.1.0) (2026-09-02)


### ⚠ BREAKING CHANGES

* **repo:** the vctui executable, Go module path, VCTUI_CONFIG/VCTUI_STATE variables, vctui config and state directories, and vctui keyring service are replaced by their vcfleet equivalents. Existing users must migrate configuration and credentials manually.

### Features

* **cache:** add a bounded, stale-preserving inventory cache; wire into the TUI ([5a4a075](https://github.com/Easonliuuuuu/vsfleet/commit/5a4a075ffa82172901f64274c279ac88b87508fb))
* **cli:** add proxy flags, wizard prompts and show output for the new routes ([e5fa27b](https://github.com/Easonliuuuuu/vsfleet/commit/e5fa27bcc8290222f90bb9ed2c9f5a204bec791f))
* **cli:** add the command line, diagnostics and cross-vCenter search ([4437247](https://github.com/Easonliuuuuu/vsfleet/commit/443724780ad0760db4312a3f11287f8ca28eeaf8))
* **internal:** add configuration, credential, transport and vSphere layers ([9117202](https://github.com/Easonliuuuuu/vsfleet/commit/911720206d043b6fe183fe4b90e02093bb916393))
* **readme:** add reproducible terminal demo ([9565598](https://github.com/Easonliuuuuu/vsfleet/commit/9565598f7e81ad48e21cd657aa245cb3caba6ee0))
* **testbed:** add synthetic vCenter development environment ([c7e0122](https://github.com/Easonliuuuuu/vsfleet/commit/c7e0122fa812d2d874b037fcec77aa7d5e50c5e6))
* **transport:** add HTTP and HTTPS CONNECT proxy routes ([fc19d91](https://github.com/Easonliuuuuu/vsfleet/commit/fc19d91798af6587bf34d469e472e9964106bd06))
* **tui:** add the terminal interface for browsing every vCenter ([3d364bc](https://github.com/Easonliuuuuu/vsfleet/commit/3d364bc4100c6d146a35ce31f168a24e22044e0a))
* **tui:** draw the row status glyph in its own gutter ([d788387](https://github.com/Easonliuuuuu/vsfleet/commit/d78838753c302991d493373f2b0cea3e0a7cc366))
* **tui:** launch the interface by default and manage contexts from it ([af00847](https://github.com/Easonliuuuuu/vsfleet/commit/af00847ee795df7fde4d76487959dc959520e8b2))
* **tui:** remember the last context, tab and sort mode between runs ([b6afa64](https://github.com/Easonliuuuuu/vsfleet/commit/b6afa649c202b11862df0528254ffa2ddce12c75))
* **tui:** support http/https proxy routes in the context form ([d56d6b8](https://github.com/Easonliuuuuu/vsfleet/commit/d56d6b892ee768d7c56bad23523af22da9f913cc))
* **vsphere:** return a partial inventory when one resource kind fails to list ([06f6f4c](https://github.com/Easonliuuuuu/vsfleet/commit/06f6f4cf766ef4012c399aac6ae1d01b66ab3807))


### Bug Fixes

* **cli_test.go:** bound the bare-command TTY test with a context deadline ([dfba606](https://github.com/Easonliuuuuu/vsfleet/commit/dfba60649640ca523072d07f0bb62191dfa709aa))
* **cli_test.go:** stop launching a real terminal program in CI ([d51cb47](https://github.com/Easonliuuuuu/vsfleet/commit/d51cb4754e040f2cde40ea81777aad4102aadea4))
* **release:** keep initial release in pre-major range ([51c01cc](https://github.com/Easonliuuuuu/vsfleet/commit/51c01cc8aefff92b30c9bc2ee19c6a962ca17d00))
* **tui:** widen the power column and drop the redundant scope marker ([c0373a4](https://github.com/Easonliuuuuu/vsfleet/commit/c0373a47adaa4ba9f5f5a4fe9db1bb67c328b2f5))
* **types.go:** pluralise the inventory count summary ([2e79c2c](https://github.com/Easonliuuuuu/vsfleet/commit/2e79c2c4a58d75348d9001b2b784b06d19112c33))
* **vsphere:** distinguish a rejected proxy password from a dead connection ([8cd540e](https://github.com/Easonliuuuuu/vsfleet/commit/8cd540e05de39e38cf0df40bd78cd095fe6255db))
* **vsphere:** name the context in interactive password prompts ([0d73d8e](https://github.com/Easonliuuuuu/vsfleet/commit/0d73d8eb83d947c9d7c43cf12a995b25a38fb320))
* **vsphere:** resolve a proxy credential once per diagnosis, not once per dialer ([319cb03](https://github.com/Easonliuuuuu/vsfleet/commit/319cb03c5e3b5a4ba7292a93586066a9a4a01b53))


### Miscellaneous Chores

* **repo:** rename project to vcfleet ([eadc588](https://github.com/Easonliuuuuu/vsfleet/commit/eadc588b2d7a15b24df803bd9ff0b7bd1c2f5f16))
