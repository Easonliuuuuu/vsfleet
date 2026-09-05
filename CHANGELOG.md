# Changelog

## [0.5.0](https://github.com/Easonliuuuuu/vsfleet/compare/v0.4.0...v0.5.0) (2026-09-05)


### Features

* **demo:** ship a credential-free demo and lead with the RVTools export ([bc77734](https://github.com/Easonliuuuuu/vsfleet/commit/bc777341669914b1fdfac064119788ec6c2825d1)), closes [#75](https://github.com/Easonliuuuuu/vsfleet/issues/75)
* **rvtools:** add the vPartition Disk Key column that joins to vDisk ([b4f9672](https://github.com/Easonliuuuuu/vsfleet/commit/b4f967233ca700088f50fe0b517601ba59c8d175))
* **rvtools:** add the vPartition tab from guest filesystem inventory ([97178f6](https://github.com/Easonliuuuuu/vsfleet/commit/97178f631d79cca68e5dbf4f5a3ef51eb3e7f36d))
* **tui:** add a comparison bar to the History Changes screen ([64e3210](https://github.com/Easonliuuuuu/vsfleet/commit/64e32100941fd484976b6390c47899d07455f253))
* **tui:** open vApp member workspace ([d0d03b9](https://github.com/Easonliuuuuu/vsfleet/commit/d0d03b9c83abac5ad41ed73d907f477100b2f746))
* **tui:** split the Changes list and its inspector side by side ([1128931](https://github.com/Easonliuuuuu/vsfleet/commit/11289319a0a3f4d393b2ad2fb76cb804a047aadd))


### Bug Fixes

* **rvtools.go:** report vDisk/vNetwork coverage from the VM collection status ([b95ceb0](https://github.com/Easonliuuuuu/vsfleet/commit/b95ceb0fa99981f73a55fc6e32a4fd2a439b70c3))
* **tui:** load large estates in pages instead of timing out at 30s ([40d9a08](https://github.com/Easonliuuuuu/vsfleet/commit/40d9a0887122e8b091620333d30512c78a8b3b44))

## [0.4.0](https://github.com/Easonliuuuuu/vsfleet/compare/v0.3.2...v0.4.0) (2026-09-04)


### Features

* **report:** add RVTools CSV export and vCPU/vMemory/vTools tabs ([0ae6c2a](https://github.com/Easonliuuuuu/vsfleet/commit/0ae6c2a2ddf7c9328876524c022a93a6ab5df084))
* **testbed:** add authenticated connected local simulator ([3153678](https://github.com/Easonliuuuuu/vsfleet/commit/3153678f36e3eb9e9167cd01314d84c3c673aeb9))


### Bug Fixes

* **assessment:** apply sqlite pragmas to every pooled connection ([01ca04e](https://github.com/Easonliuuuuu/vsfleet/commit/01ca04e51183de006dfdf990ebb2b36b6ecb2710))
* **tui:** defer interactive credentials until explicit load ([ed23bc0](https://github.com/Easonliuuuuu/vsfleet/commit/ed23bc09f711b46c3836324ae8265ef839f01895))
* **tui:** give the history hub one meaning for n and honest pane hints ([f567694](https://github.com/Easonliuuuuu/vsfleet/commit/f5676945c9ed0565086b6f1ef0fbe6c0951e85da))
* **tui:** scope history capture to the vCenter in scope and gate its prompts ([40aed9b](https://github.com/Easonliuuuuu/vsfleet/commit/40aed9bc3d76e0116bc1b05c2cfcef5630dfe6be))

## [0.3.2](https://github.com/Easonliuuuuu/vsfleet/compare/v0.3.1...v0.3.2) (2026-09-04)


### Bug Fixes

* **tui:** keep vApps and history discoverable ([ee1bda7](https://github.com/Easonliuuuuu/vsfleet/commit/ee1bda7b09608fce895cf579be29c5b4aad01d87))
* **tui:** show loading pane before startup credential prompt ([07560c3](https://github.com/Easonliuuuuu/vsfleet/commit/07560c38f81b931e101f0946d1e687562caf82d2))
* **tui:** stop unsolicited cross-context password prompts ([5c784d4](https://github.com/Easonliuuuuu/vsfleet/commit/5c784d4c3b069ca67e064697d5d57bb243838f81))

## [0.3.1](https://github.com/Easonliuuuuu/vsfleet/compare/v0.3.0...v0.3.1) (2026-09-04)


### Bug Fixes

* **release:** match Homebrew cask to the archive id ([61e42a2](https://github.com/Easonliuuuuu/vsfleet/commit/61e42a2850c345bdcbd665b87b4b7adb8b75dce0))

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
