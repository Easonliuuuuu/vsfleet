# Contributing to vsfleet

Thank you for your interest in contributing to vsfleet! Whether you are fixing
a bug, adding a new resource view, improving documentation, or optimizing
performance, your help is welcome.

---

## Code of Conduct

Please help us keep this project respectful and welcoming to everyone. Be kind,
constructive, and thoughtful in all discussions, issues, and pull requests.

---

## Architectural Principles

Before contributing code, please review [docs/architecture.md](docs/architecture.md).
Any new feature or change must uphold our core invariants:

1. **Strict Read-Only Safety:** vsfleet is an inspection tool. We do not accept
   mutating operations (power control, snapshot management, provisioning, or
   deletion).
2. **Partial Failure Tolerance:** Failures in one vCenter must never terminate
   queries for other configured vCenters.
3. **Zero Plaintext Credentials on Disk:** Passwords belong in OS keyrings or
   volatile memory, never in `config.toml`.

---

## Development Prerequisites

* **Go:** Version 1.25 or newer.
* **Git:** Version 2.25 or newer.
* **Optional:** [VHS](https://github.com/charmbracelet/vhs) for regenerating
  terminal demo GIFs.

---

## Local Development & Testing

### 1. Running Tests and Checks

Before opening a pull request, ensure all tests, race detectors, and linters pass:

```bash
# Run tests with race detection
go test -race ./...

# Run static analysis
go vet ./...

# Verify formatting
test -z "$(gofmt -l .)"
```

The integration tests in `tests/` use VMware's built-in `govmomi/simulator` and
in-memory proxy servers, so they run completely offline without requiring real
vCenter credentials.

---

## Developing with the Synthetic Testbed

You do **not** need access to real vCenter instances or ESXi hardware to develop
features or work on the terminal interface.

vsfleet includes a deterministic, synthetic presentation backend:
* **Binary:** `cmd/vsfleet-demo`
* **Fixtures:** `internal/demo/backend.go`

### Launch the Demo TUI

Run the synthetic demo directly:

```bash
go run ./cmd/vsfleet-demo
```

This launches the full Bubble Tea interface against three simulated vCenters
(`prod-vc`, `edge-vc`, and `dr-site`), populated with sample VMs, templates,
hosts, clusters, vApps, datastores, and networks.

* `prod-vc`: Represents a healthy, direct-connected primary site.
* `edge-vc`: Represents an edge environment reached via proxy.
* `dr-site`: Represents an intentionally unreachable site (proxy connection
  refused), allowing you to test partial failure handling and diagnostics.

### Modifying Demo Fixtures

When adding support for new resource kinds or states, add sample data to
`internal/demo/backend.go`. Ensure fixtures remain:
* Deterministic (no random UUIDs or shifting timestamps).
* Visibly synthetic (using `.internal` or `.example` domains).
* Free of real endpoints, secrets, or customer data.

---

## Updating the README Demo

The animated demo in the README is recorded using [VHS](https://github.com/charmbracelet/vhs).
If you make visual changes to the TUI, regenerate both the GIF and the
reduced-motion PNG:

```bash
vhs docs/demo.tape
```

This tape builds `cmd/vsfleet-demo` as a temporary binary, executes the scripted
key strokes, and updates `docs/assets/vsfleet.gif` and `docs/assets/vsfleet.png`.

## Publishing Releases

Releases are built by GoReleaser from the tag created by `release-please`. The
workflow publishes the archives to GitHub Releases and updates the
`Easonliuuuuu/homebrew-tap` repository with the matching Homebrew cask.

The repository's `TAP_GITHUB_TOKEN` secret must be a GitHub token with
write access to `Easonliuuuuu/homebrew-tap`; the default Actions token cannot
write to a separate tap repository.

---

## Commit Guidelines

vsfleet uses [Conventional Commits](https://www.conventionalcommits.org/) to
automate versioning and changelog generation via Google's `release-please`.

Please format commit messages as:

```text
<type>(<scope>): <short description>
```

Common types:
* `feat`: A new user-facing feature or command
* `fix`: A bug fix
* `docs`: Documentation updates
* `refactor`: Internal code refactoring without behavior changes
* `test`: Adding or modifying tests
* `chore`: Build tasks, dependency updates, or tool configurations

Examples:
* `feat(cli): add vapp list command`
* `fix(transport): handle SOCKS5 remote DNS timeout`
* `docs(readme): add installation options for pre-built binaries`

---

## Submitting Pull Requests

1. Fork the repository and create your branch from `main`:
   ```bash
   git checkout -b feat/my-new-feature
   ```
2. Keep changes focused and atomic.
3. Ensure all tests and linters pass (`go test -race ./...`).
4. Push your branch and open a pull request against `main`.
5. Maintainers will review your PR and provide constructive feedback!
