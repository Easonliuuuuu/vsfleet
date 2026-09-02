---
name: testbed
description: Prepare, launch, verify, or extend VSFleet's isolated synthetic-vCenter testbed. Use for local UI development, demos, screenshots, fixture changes, or requests to run VSFleet without real vCenter credentials. Do not use for live-vCenter setup or claim that the presentation backend exposes VMware API endpoints.
---

# VSFleet testbed

Use the repository's `cmd/vsfleet-demo` command and `internal/demo` backend as the default testbed. They provide the real terminal UI over deterministic, read-only inventory without reading operator configuration, credentials, keyrings, or the network.

## Choose the operation

- **Prepare or launch locally:** run `scripts/make-testbed.sh` from this skill. Default to `--mode companion-local`; use `--mode launch` when the user wants the UI opened immediately.
- **Make `vsfleet` resolve in the user's current shell:** have the user run the exact `eval` command printed by companion-local setup, or `eval "$(.../make-testbed.sh --mode shell-hook --repo <repo>)"`. A subprocess cannot change the parent shell's `PATH`; say this plainly.
- **Verify the testbed:** run the helper with `--mode verify`. For UI changes, also perform the short interactive checks in [references/estate.md](references/estate.md).
- **Change the synthetic estate:** read [references/estate.md](references/estate.md), edit `internal/demo/backend.go`, add focused tests when behavior or invariants change, then verify.
- **Install persistently:** first inspect `type -a vsfleet` and the user's shell. Obtain explicit approval before writing outside the repository or editing a shell startup file. Never overwrite an existing `vsfleet`; install the testbed in its own directory and prepend that directory only through a clearly labeled, removable shell-profile block.
- **Provide actual VMware API endpoints:** the presentation backend is not an HTTP/SOAP vCenter. If the user explicitly needs CLI integration against connectable endpoints, treat that as a separate govmomi simulator mode. Bind only to loopback, use isolated config and credentials, document start/stop behavior, and keep it distinct from the default presentation testbed.

## Local workflow

1. Resolve the repository root and confirm it contains `go.mod`, `cmd/vsfleet-demo`, and `internal/demo/backend.go`.
2. Inspect `git status --short` before changing fixtures. Preserve unrelated work.
3. Build an isolated companion launcher:

   ```bash
   .agents/skills/testbed/scripts/make-testbed.sh --mode companion-local --repo .
   ```

4. Report the generated launcher path and the exact shell-hook command. The generated binary is named `vsfleet`, but it is the demo command and never the production command.
5. If requested, launch it in a real TTY:

   ```bash
   .agents/skills/testbed/scripts/make-testbed.sh --mode launch --repo .
   ```

The helper writes only beneath its output directory, uses a marker before replacing a prior generated launcher, and defaults to a user-specific directory under `${TMPDIR:-/tmp}`. Do not redirect it onto a production binary or silently alter global `PATH`.

## Safety and fidelity

- Keep the default testbed deterministic, offline, read-only, and visibly synthetic.
- Never copy real endpoints, passwords, thumbprints, configuration, or inventory into fixtures.
- Preserve the intentionally unhealthy context; it exercises partial-failure rendering and diagnosis.
- Do not make add/edit/remove appear successful unless the user explicitly asks to turn the testbed into a writable simulator and the semantics are implemented truthfully.
- Keep the command name separation in source: `cmd/vsfleet` is production and `cmd/vsfleet-demo` is synthetic. Naming only the generated companion binary `vsfleet` is what makes the shell experience realistic.
- Treat screenshots and recordings as testbed artifacts. Do not imply that synthetic hosts are live infrastructure.

## Completion criteria

Run `scripts/make-testbed.sh --mode verify --repo <repo>`. Then report:

- whether the Go test suite and demo build passed;
- the isolated launcher path;
- how the user can make `vsfleet` resolve for the current shell;
- whether the testbed is presentation-only or includes an explicitly requested API simulator;
- any manual TUI behavior that was not exercised.
