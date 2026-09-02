# Changelog

## [0.1.0](https://github.com/Easonliuuuuu/VC-TUI/compare/v0.0.1...v0.1.0) (2026-09-02)


### Features

* **cache:** add a bounded, stale-preserving inventory cache; wire into the TUI ([5a4a075](https://github.com/Easonliuuuuu/VC-TUI/commit/5a4a075ffa82172901f64274c279ac88b87508fb))
* **cli:** add proxy flags, wizard prompts and show output for the new routes ([e5fa27b](https://github.com/Easonliuuuuu/VC-TUI/commit/e5fa27bcc8290222f90bb9ed2c9f5a204bec791f))
* **cli:** add the command line, diagnostics and cross-vCenter search ([4437247](https://github.com/Easonliuuuuu/VC-TUI/commit/443724780ad0760db4312a3f11287f8ca28eeaf8))
* **internal:** add configuration, credential, transport and vSphere layers ([9117202](https://github.com/Easonliuuuuu/VC-TUI/commit/911720206d043b6fe183fe4b90e02093bb916393))
* **transport:** add HTTP and HTTPS CONNECT proxy routes ([fc19d91](https://github.com/Easonliuuuuu/VC-TUI/commit/fc19d91798af6587bf34d469e472e9964106bd06))
* **tui:** add the terminal interface for browsing every vCenter ([3d364bc](https://github.com/Easonliuuuuu/VC-TUI/commit/3d364bc4100c6d146a35ce31f168a24e22044e0a))
* **tui:** draw the row status glyph in its own gutter ([d788387](https://github.com/Easonliuuuuu/VC-TUI/commit/d78838753c302991d493373f2b0cea3e0a7cc366))
* **tui:** launch the interface by default and manage contexts from it ([af00847](https://github.com/Easonliuuuuu/VC-TUI/commit/af00847ee795df7fde4d76487959dc959520e8b2))
* **tui:** remember the last context, tab and sort mode between runs ([b6afa64](https://github.com/Easonliuuuuu/VC-TUI/commit/b6afa649c202b11862df0528254ffa2ddce12c75))
* **tui:** support http/https proxy routes in the context form ([d56d6b8](https://github.com/Easonliuuuuu/VC-TUI/commit/d56d6b892ee768d7c56bad23523af22da9f913cc))
* **vsphere:** return a partial inventory when one resource kind fails to list ([06f6f4c](https://github.com/Easonliuuuuu/VC-TUI/commit/06f6f4cf766ef4012c399aac6ae1d01b66ab3807))


### Bug Fixes

* **cli_test.go:** bound the bare-command TTY test with a context deadline ([dfba606](https://github.com/Easonliuuuuu/VC-TUI/commit/dfba60649640ca523072d07f0bb62191dfa709aa))
* **cli_test.go:** stop launching a real terminal program in CI ([d51cb47](https://github.com/Easonliuuuuu/VC-TUI/commit/d51cb4754e040f2cde40ea81777aad4102aadea4))
* **release:** keep initial release in pre-major range ([51c01cc](https://github.com/Easonliuuuuu/VC-TUI/commit/51c01cc8aefff92b30c9bc2ee19c6a962ca17d00))
* **tui:** widen the power column and drop the redundant scope marker ([c0373a4](https://github.com/Easonliuuuuu/VC-TUI/commit/c0373a47adaa4ba9f5f5a4fe9db1bb67c328b2f5))
* **types.go:** pluralise the inventory count summary ([2e79c2c](https://github.com/Easonliuuuuu/VC-TUI/commit/2e79c2c4a58d75348d9001b2b784b06d19112c33))
* **vsphere:** distinguish a rejected proxy password from a dead connection ([8cd540e](https://github.com/Easonliuuuuu/VC-TUI/commit/8cd540e05de39e38cf0df40bd78cd095fe6255db))
* **vsphere:** name the context in interactive password prompts ([0d73d8e](https://github.com/Easonliuuuuu/VC-TUI/commit/0d73d8eb83d947c9d7c43cf12a995b25a38fb320))
* **vsphere:** resolve a proxy credential once per diagnosis, not once per dialer ([319cb03](https://github.com/Easonliuuuuu/VC-TUI/commit/319cb03c5e3b5a4ba7292a93586066a9a4a01b53))
