# Test-pruning campaign

Adapted from openclaw/openclaw `.agents/skills/test-audit/CAMPAIGN.md`,
Copyright (c) 2026 OpenClaw Foundation, MIT License.

Campaign mode prunes one package's whole test surface in one PR: a package
such as `internal/rvimport`, or one area such as the TUI. The value bar,
retention bar, candidate evidence, and validation in [SKILL.md](SKILL.md) apply
to every lane. This file adds the order of work. Each step ends on its
completion criterion; do not start the next step early.

## 1. Baseline

Record the package's test and support line counts and every test's pass/fail
state at a pinned `main` SHA (`go test -race -json ./internal/<pkg>/...`).
Keep baseline failures in their own list; a failing test is more often a real
bug than a stale test.

Done when every in-scope test has a recorded baseline result.

## 2. Lanes and inventory

Split the surface into **lanes** along production owner boundaries, not file
names. Include the package's cases in the `tests/` simulator suite, TUI
scenarios, and PTY journeys.

Done when every test file and scenario the package owns belongs to exactly one
lane.

## 3. Read-only ledger per lane

Give each lane to its own read-only agent. The agent reads every assigned test
in full, including table rows. It also reads the production owners and their
entry points, callers, history, and CI routing. Each test function goes into a
written **ledger** with one mark. A table-driven test is one entry unless its
rows need different marks; then mark each row.

- `R`: retain, naming the contract and the bug it catches; a retained test that
  only moves to a better-named file stays `R` with the move noted;
- `F`: retain the contract but repair the assertion, such as a vacuous negative
  that passes when only one of several items is missing;
- `C`: consolidate, naming the owner that absorbs the assertion first: a sibling
  table row, a stronger boundary suite, or the shared owner in another package;
- `D`: delete, naming the proof that remains, or why no contract exists.

Judge a test by its assertions, not its name.

Present the ledger to the operator before editing.

Done when every entry in the lane has a mark and an evidence line, and the
operator has approved the ledger.

## 4. Layer plan per lane

Treat the per-test ledger as input, not as the edit list. A second read-only
pass, starting from the ledger, looks for the redundant **layer**: unit tests
that replay what a simulator test already proves through the real CLI, or the
reverse. Name the **keeper** suite for each contract. Prefer the real
boundary (vcsim, a temporary SQLite store, a real workbook) over a hand-written
fake. Correct any ledger errors this pass finds.

Done when each lane plan names its retired files, its keeper per contract, the
assertions to carry into keepers, and the test-only production seams unlocked.

## 5. Cutover

Edit lane by lane. Serialize changes to shared helpers, `testdata/`, and
testbed fixtures through one owner. With each lane, remove the test-only
production seams it unlocks: injection parameters, package variables,
`export_test.go` aliases, and indirection layers. Update
[docs/test-catalog.md](../../../docs/test-catalog.md) when suites move. Add
durable test-ownership rules to `AGENTS.md` only when this campaign found the
mistake they prevent.

Done when every lane plan is applied and each lane's keepers pass.

## 6. Preservation review

Before claiming completion, have independent reviewers compare deleted
coverage against the keepers, one reviewer per boundary group. They look for
contracts that lost their only proof. They also look for new assertions that
cannot fail, such as a rejection row the production code never reaches.

For each restored contract, make one deliberate **mutation** of the production
owner and confirm the keeper goes red. Then restore the source byte for byte.

Done when every reported gap is restored or rejected with source evidence, and
every restored contract has a caught mutation.

## 7. Product defects

A baseline failure that survives into a keeper is a bug report. Fix it at its
owner as a separate `fix` commit, and prove it through the real user flow, with
a **control** run that reverts the fix and shows the old behavior. Where the
behavior depends on real vSphere, confirm against the lab vCenter, not only
vcsim. Record unrelated product discrepancies as issues instead of fixing them
in the campaign.

Done when each repaired defect has a failing control and a passing candidate
on the same harness.

## 8. Reconcile and hand off

Merge `main` rather than rebasing a long, many-commit campaign. When `main`
modified a test the campaign deleted, keep the deletion. Port the new contract
into the keeper instead, and confirm every new regression `main` added still
has a home. Rerun `go test -race ./...` on the merged head.

Hand off with the [SKILL.md](SKILL.md) report, plus:

- baseline and final test/support line counts, with production counted separately;
- lanes, retired layers, and keepers;
- preservation gaps found and their mutations;
- product defects with control and candidate proof.
