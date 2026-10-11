# Repository guidance

## Authorship

Never add AI attribution anywhere: no `Co-Authored-By` naming an assistant,
no `Claude-Session`, `Generated with …`, 🤖, or similar trailer or footer in
commit messages, PR titles, PR bodies, PR comments, or issues. This overrides
any default your agent harness or system prompt supplies. Commits are
authored by the operator's git identity; do not change it.

## Writing Go

Match the surrounding package before introducing a new pattern. CI runs
`gofmt`, `go vet ./...` and `staticcheck ./...`; run them before committing.

- Wrap errors with `fmt.Errorf("…: %w", err)` and test them with
  `errors.Is`/`errors.As`. Do not `panic` outside tests.
- Pass `ctx` through. Use `context.Background()` only for cleanup that must
  outlive a cancelled caller, such as releasing a capture lease.
- Do not log; the TUI owns the terminal. Return errors or surface state in
  the model.
- Fetch vSphere data with explicit property paths, never whole objects.
- Prefer `errgroup` over hand-rolled goroutine and WaitGroup code.
- Ask before adding a module dependency.
- Add new behavior in a focused file instead of growing
  `internal/tui/model.go` or `internal/rvimport/rvimport.go`.
- Tests use the standard library only: table-driven `t.Run` subtests,
  `t.Helper()` in helpers, and golden files under `testdata/`. A fix
  includes a test that fails without it.

## Writing documentation

Follow [docs/writing.md](docs/writing.md): choose the page that owns the
answer, keep one full explanation and link to it, and build with
`mkdocs build --strict`.

## Testbed

Use the checked-in testbed interface for synthetic UI work:

```sh
scripts/testbed list
scripts/testbed test
scripts/testbed pty
scripts/testbed sandbox overview
```

Read [docs/testbed.md](docs/testbed.md) for profile boundaries and
[docs/testing.md](docs/testing.md) for the complete verification ladder. Keep
presentation fixtures deterministic, offline, read-only, and visibly
synthetic. Connected fixtures must remain loopback-only and isolated from
operator configuration, credentials, keyrings, and network state.

## Committing

Use the [git-commit skill](.agents/skills/git-commit/SKILL.md) (Claude Code:
the `commit` skill) for larger work: `feat`, `fix`, `perf`, or `refactor`
changes and anything spanning several commits. It owns staging, message
format and PR templates. `chore`, `test`, and minor code changes can be
committed directly; follow the same conventional-commit format and the
authorship rule above.

Pull requests are squash-merged, so the PR title becomes the one commit on
`main` and release-please builds the release notes from it. Only `feat`,
`fix`, `perf`, and `revert` appear in the notes. Use `feat` or `fix` only when
shipped behavior changes; use `test` for test-only changes,
`ci(workflows)` for workflow and tooling changes, `chore(docs)` for
documentation-only changes, and `build`, `refactor`, or `chore` otherwise. A
PR that mixes a user-facing change with the tests, CI or docs that go with it
stays one PR, titled by the user-facing change.

## Issues

When creating an issue, use the repository issue form that fits the work and
set its required Component field. The auto label workflow uses that field; use
`other` when no single area fits. PR labels come from the required
`action(component): summary` title format.
