---
name: git-commit
description: Stage and commit all changes using conventional commits format, then optionally push and open a PR with a matching template. Use when the user wants to commit current working tree changes with a standardized message, or also wants a PR opened.
license: MIT
metadata:
  author: local
  version: "1.2"
---

Stage and commit all changes using the conventional commits format, then optionally push the branch and open a PR whose description mirrors the commit body.

**Input**: Optional scope or hint from the user (e.g., "socks transport", "context wizard"). If omitted, derive everything from the diff.

---

**Authorship**

Commits and PRs are authored by the human running the work. An AI assistant is a tool used to produce the change, not a party to it.

- The commit **author** and **committer** must both be the repository user's own git identity (`user.name` / `user.email`). Never set `GIT_AUTHOR_*` or `GIT_COMMITTER_*`, and never pass `--author`, to an assistant, bot, or `noreply` AI identity.
- Never add a `Co-Authored-By` line naming an AI assistant, in a commit message or anywhere else.
- Never add `Claude-Session`, `Generated with ...`, `🤖`, or any similar assistant attribution trailer or footer to a commit message, a PR title, a PR body, or a PR comment.
- This applies to every commit and PR in this repository, including ones created by an agent on the user's behalf.

---

**Steps**

1. **Inspect the working tree**

   Run in parallel:
   ```bash
   git status
   git diff HEAD
   git diff --staged
   git log --oneline -5
   ```

   Use these to understand:
   - Which files are modified/added/deleted (staged and unstaged)
   - What the actual changes are
   - The recent commit style of this repo

2. **Derive the conventional commit message**

   **Type** — pick exactly one:
   - `feat` — new capability visible to users or API consumers
   - `fix` — corrects a bug or broken behavior
   - `refactor` — restructures code without changing behavior
   - `chore` — dependency updates, config, tooling, cleanup with no behavior change
   - `docs` — documentation only
   - `test` — adds or fixes tests
   - `ci` — CI/CD pipeline changes
   - `build` — build system, Dockerfile, packaging

   **Breaking changes** — mark them explicitly whenever the change breaks an existing public API, CLI flag, config shape, or other consumer-facing contract:
   - Add `!` right after the type/scope: `feat(config)!: drop support for the version 1 configuration format`
   - And add a `BREAKING CHANGE: <description>` footer at the end of the body explaining what breaks and how to migrate.

   **Scope** (in parentheses) — the bare name (no path, no slash) of the package, folder, or file most responsible for the change, e.g. `transport`, `client.go`, `ci.yml`. Use a single file's name when the change is concentrated there; use the containing package's name when several files in it changed together. Omit only when changes span unrelated top-level areas with no shared package.

   **Summary** — imperative mood, lowercase, no period, ≤72 chars total for the first line. Describe what the change *does*, not what files changed.

   **Body** — include for any non-trivial change. Separate from the summary with a blank line. Use the following three sections:

   ```
   Changes:
   - <bullet describing each meaningful change; one line per logical unit>

   Root cause: <one or two sentences — WHY this change was needed (bug, design issue, obsolete dependency, broken test, etc.). Omit for pure feature additions; use "Motivation:" instead to explain the why.>

   Testing: <how correctness was verified — e.g., "go test ./... passed", "go test -race ./... passed, including tests/network_test.go", "manual run against vcsim", or "no tests — trivial config change">
   ```

   Omit the body only for single-line no-brainers (typo fix, rename, version bump). For everything else, always include all three sections.

   Do not add a `Co-Authored-By` (or similar) footer to the commit message. See **Authorship** above.

   **Examples:**
   ```
   fix(credentials): serialise prompt reads across concurrent connections

   Changes:
   - prompt.go: guard ReadLine and ReadSecret with a mutex
   - client.go: name the context in the prompt label

   Root cause: commands that span the estate connect to every context
   concurrently, so two goroutines wrote prompts and read answers through the
   same reader, interleaving the output and corrupting the input.

   Testing: go test -race ./... passed; previously reported a data race in
   credentials.(*Prompt).ReadSecret
   ```
   ```
   ci(ci.yml): run gofmt, vet, staticcheck and tests on pushes and pull requests

   Changes:
   - ci.yml: add a build matrix over Linux, macOS and Windows
   - ci.yml: verify gofmt cleanliness and that go.mod is tidy
   - ci.yml: run the suite under -race and add a staticcheck job

   Motivation: nothing currently catches a broken build, an untidy go.mod, or a
   data race before it lands on main.

   Testing: workflow runs green on the PR that introduces it
   ```
   ```
   chore(go.mod): bump govmomi to 0.56.0
   ```
   ```
   feat(cli)!: require --context instead of falling back to the only context

   Changes:
   - root.go: remove the single-context fallback, require an explicit --context

   Motivation: the fallback silently picked a different vCenter once a second
   context was added; an explicit flag removes the ambiguity.

   BREAKING CHANGE: --context is now required when more than one context is
   configured. Scripts relying on the fallback will fail until they pass it.

   Testing: go test ./... passed, including internal/config
   ```

3. **Stage changes**

   Add specific files by name — never `git add -A` or `git add .` blindly.

   - List all modified/untracked files from `git status`
   - Skip files that look like secrets (`.env`, `*credentials*`, `*secret*`, `config.toml`) — warn the user instead
   - Stage everything else:
     ```bash
     git add <file1> <file2> ...
     ```

4. **Show the proposed commit and ask for confirmation**

   Display:
   ```
   ## Proposed commit

   <full commit message>

   Files staged: <count>
   ```

   Use **AskUserQuestion tool** with options: "Commit", "Edit message", "Cancel"

   - If "Edit message": ask the user for their preferred message, then re-display and confirm once more
   - If "Cancel": stop, leave files staged
   - If "Commit": proceed

5. **Commit**

   ```bash
   git commit -m "$(cat <<'EOF'
   <type>(<scope>): <summary>

   Changes:
   - <change 1>
   - <change 2>

   Root cause: <why>

   Testing: <how verified>
   EOF
   )"
   ```

   For trivial single-line commits (no body):
   ```bash
   git commit -m "<type>(<scope>): <summary>"
   ```

6. **Display result**

   Run `git log --oneline -1` and show the commit hash + message.

   ```
   ## Committed

   <hash> <message>
   ```

7. **Offer to push and open a PR**

   Use **AskUserQuestion** with options: "Push & open PR", "Just commit (done)"

   If "Just commit (done)": stop here.

   If "Push & open PR":

   a. Check the current branch with `git branch --show-current`. If it's `main` (or the repo's default branch), create a new branch first — derive a short kebab-case name from the commit's `<type>(<scope>)` (e.g. `fix/credentials-prompt-race`), or ask the user for one if nothing sensible can be derived:
      ```bash
      git checkout -b <branch-name>
      ```

   b. Push and set upstream:
      ```bash
      git push -u origin <branch-name>
      ```

   c. Build the PR title and body from the same material as the commit message — do not re-derive from scratch:
      - **Title**: the same conventional-commit format as step 2 — `<type>(<scope>): <summary>` — identical to the commit's first line. The parenthesised scope is **required** on a PR title: a title without it (`fix: ...`) or in prose form (`Fix the worktree race`) is not acceptable. Use the same `<type>` values: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `ci`, `build`.
        Examples: `feat(search): query every configured vCenter concurrently`, `fix(credentials): serialise prompt reads across concurrent connections`, `refactor(inventory.go): resolve paths from one name/parent index`.
        When a PR carries several commits, the title describes the PR as a whole, still as `<type>(<scope>): <summary>`.
      - **Body** — reuse the commit's `Changes`, `Root cause`/`Motivation`, and `Testing` sections, and add a `Test plan` checklist (concrete, reviewer-actionable steps derived from `Testing`):

        ```
        ## Summary
        - <bullet from commit's Changes section>
        - <bullet from commit's Changes section>

        ## Root cause
        <or "## Motivation" — same content as the commit body>

        ## Test plan
        - [ ] <concrete step derived from Testing>
        - [ ] <concrete step derived from Testing>

        ## Testing
        <same content as the commit's Testing line>
        ```

      Never add a `Co-Authored-By`, "Generated with Claude Code", `Claude-Session`, or similar footer to the PR title or body. See **Authorship** above.

   d. Show the proposed PR title + body and confirm with **AskUserQuestion** ("Create PR", "Edit", "Cancel") before running:
      ```bash
      gh pr create --title "<title>" --body "$(cat <<'EOF'
      <body>
      EOF
      )"
      ```

      An agent without `gh` available uses the equivalent GitHub API tool, with the same title, the same body, and the same prior confirmation.

   e. Display the returned PR URL.

**Guardrails**
- Never use `git add -A` or `git add .`
- Never skip pre-commit hooks (`--no-verify`)
- Never commit `.env` or credential files — warn and skip them
- Never name an AI assistant as author, committer, or co-author, and never add an assistant attribution footer to a commit message, PR title, PR body, or PR comment
- If `git commit` fails due to a hook, report the hook output and stop; do not retry automatically
- If there are no changes to stage, report "Nothing to commit" and stop
- Always use HEREDOC syntax for multi-line commit messages and PR bodies to preserve formatting
- Never push directly to `main`/the default branch — always create a feature branch first
- Never force-push (`--force`/`-f`) as part of this flow
- Only run `gh pr create` after the user has explicitly confirmed the PR title/body
