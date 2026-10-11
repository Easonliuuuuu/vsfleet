---
name: test-audit
description: "Invoke whenever writing, changing, reviewing, or sweeping Go tests. Authoring gate for new tests plus audit workflow for low-value, implementation-coupled, or duplicative tests and the test-only production seams they demand."
license: MIT
metadata:
  author: local
  version: "1.0"
  source: "Adapted from openclaw/openclaw .agents/skills/test-audit, Copyright (c) 2026 OpenClaw Foundation, MIT License"
---

# Test Audit

Three modes, one value bar. Authoring mode gates every new or changed test at
write time. Audit mode runs focused sweeps of tests that re-assert source,
duplicate stronger proof, couple behavior to implementation, or keep test-only
production seams alive. Continue broad audits as separate coherent follow-up
PRs; optimize for confidence, not deletion count. Campaign mode prunes one
whole package's test surface; before starting one, read
[CAMPAIGN.md](CAMPAIGN.md).

Audit and campaign modes are report-first: present candidates with evidence
and wait for the operator's approval before deleting or rewriting any test.

## Authoring gate

Before adding any test, answer four questions; a missing answer means do not
add it yet:

1. What observable behavior, invariant, or independent contract does it protect?
2. What credible regression makes it fail?
3. Why does existing coverage not already catch that failure? Each contract has
   one primary test owner at the strongest boundary; another layer needs its
   own distinct risk, such as a transport or lifecycle failure the owner cannot
   reach. Prefer adding a row to an existing table-driven test or shared
   fixture over a near-duplicate test; consolidate duplicated setup in the same
   change.
4. Does it need a production seam (exported helper, `export_test.go` alias,
   package variable, interface, or injection hook) that no production caller
   needs? If yes, move the test to the real boundary instead.

For which tier owns a behavior (unit, in-process simulator, TUI scenario, PTY
journey, lab), use [docs/testing.md](../../../docs/testing.md) and the
[test catalogue](../../../docs/test-catalog.md).

Then check the test against every [junk pattern](#junk-patterns); a match fails
the gate unless the [retention bar](#retention-bar) names the contract it
independently guards. A test that would break under behavior-preserving
refactoring is asserting implementation, not behavior; rewrite it at the
owning boundary before landing it.

Bug regression tests must fail on the pre-fix code for the intended reason and
pass after the owner-boundary repair. A regression test that never demonstrably
failed proves the fake, not the fix. One regression at the owner boundary
covers the bug; do not replay the same scenario at every layer it crosses.

## Junk patterns

The shared checklist for both modes: the authoring gate rejects a new test that
matches one, and audits hunt for existing tests that do.

- assertion-free coverage probes, including tests that only check
  `err == nil` on a call whose result is never inspected;
- self-comparisons and identity copiers;
- copied fixtures, inventories, column lists, or flag lists that restate a
  table in the source;
- exact source, import, or string greps;
- unexported predicate or call-shape tests duplicated at real boundaries;
- duplicate invocations of the same contract, including table rows that
  differ only in values the code never branches on;
- tests that re-verify a dependency (cobra flag parsing, `strconv`,
  `encoding/json`, bubbletea key handling) rather than this repository's use
  of it;
- tests whose only purpose is preserving test-only exports, globals, or
  wrappers;
- dead production code whose only callers are tests;
- expected values or golden files produced by the helper or renderer under
  test and never reviewed against an independent source;
- fakes that implement the asserted behavior, or one identical fake standing
  in for different APIs;
- simulator (vcsim) assertions on behavior vcsim invents and real vCenter does
  not share, presented as vSphere proof;
- negative controls that pass for an unrelated reason, such as an error from a
  different guard or a rejection the production path never reaches;
- names or fixtures that promise more than the input exercises.

## Value bar

Tests justify their maintenance cost by protecting behavior, a credible
regression, or an independently meaningful contract. In an audit, an existing
test that must change for behavior-preserving source reorganization is suspect,
not automatically deletable; the authoring gate still rejects new ones.

Before judging a candidate, read the complete test and production owner, its
entry point, callers, callees, sibling implementations, overlapping tests, CI
routing, and relevant history (`git log -L`, the PR that added it, the issue it
references). Read `AGENTS.md` first. When the test claims govmomi- or
dependency-backed behavior, inspect the dependency source or types directly.

## Discovery

Keep discovery read-only and report evidence before editing. For broad scope,
run parallel discovery lanes when available:

- vSphere collection and transport (`internal/vsphere`, `internal/transport`,
  `internal/session`, `internal/credentials`);
- assessment, import, and export (`internal/assessment`, `internal/rvimport`,
  `internal/report`, `internal/metareport`);
- TUI and CLI (`internal/tui`, `internal/cli`, `cmd/`);
- simulator, testbed, and PTY suites (`tests/`, `internal/testbed`);
- a cross-cutting pattern sweep.

Outside campaign mode, prefer a few high-confidence candidates over a large
speculative inventory. Hunt for the [junk patterns](#junk-patterns).

When it is installed, mutation testing gives evidence instead of opinion:
`gremlins unleash ./internal/<pkg>` lists mutants that no test kills. A
surviving mutant points at a weak or missing assertion, not at a deletable
test.

## Retention bar

Keep a test when it independently enforces a CLI flag, exit code, output
format, config key, export column or workbook layout, RVTools import mapping,
capture schema or migration, credential storage or redaction, read-only SOAP
guarantee, supported vSphere version, release, or architecture contract. Also
keep:

- call ordering when order is observable behavior;
- regressions with a credible failure mode, especially those tied to an issue;
- golden files that pin a user-visible rendering reviewed by a human;
- source inspection when it is the cheapest independent guard: it fails when
  the contract changes (the user-facing key, column, or path) and survives an
  identifier-only refactor;
- a retained test that fails on the baseline: treat it as a possible product
  bug, reproduce it, and repair the owner rather than deleting it.

Static or slow is not a deletion reason. A test that resembles implementation
may still be the independent contract; prove otherwise before removing it.

## Candidate evidence

Record every field below before editing. A missing field means the candidate is
not ready for deletion:

- exact test name and location (`file_test.go:line`, subtest name);
- what failure it can actually detect;
- non-test callers of the covered production or support seam;
- stronger remaining owner-boundary proof, or why no proof is needed;
- relevant history and the reason the test or seam exists;
- production or test-support deletion unlocked;
- risk and the focused validation command.

## Edit shape

Choose one coherent owner-boundary batch. Delete obsolete test-only exports,
globals, wrappers, and dead production paths instead of preserving aliases.
Move retained regressions to their canonical owners. Consolidate repeated
assertions into one table-driven test.

Prefer net-negative production LOC. Do not add replacement tests that restate
the same implementation, and do not convert uncertain candidates into cleanup
to increase deletion counts.

## Validation

1. Run the smallest owner and sibling tests:
   `go test -race ./internal/<pkg>/...`, then `go test -race ./tests/...` when
   the simulator suite covers the same path.
2. For removed golden-file or source assertions, run the command or testbed
   scenario that owns the real contract (`scripts/testbed test`).
3. Run `gofmt -l .`, `go vet ./...`, `staticcheck ./...`, and
   `git diff --check`.
4. Run `go test -race ./...` before handing off.
5. Inspect `git diff --numstat`; report production code separately from tests
   and test support.
6. After final audit edits, run the `code-review` skill on the diff.

## Landing and continuation

Commit, push, open a PR, or land only when authorized. Use the
[git-commit skill](../git-commit/SKILL.md) with a `test` title, or `refactor`
when production seams are removed. Land one coherent PR at a time; after
landing, refresh from current `main` and rerun read-only discovery for the next
high-confidence batch.

## Handoff

Report:

- removed low-value categories and why each was low value;
- production owner simplifications;
- retained false positives and why they remain valuable;
- focused and full proof actually run;
- production versus test LOC;
- PR and merge state;
- named follow-ups.
