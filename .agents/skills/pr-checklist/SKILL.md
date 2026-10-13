---
name: pr-checklist
description: Check a change before opening a PR or asking for another review, or analyze an open PR. Covers design fit with IRO's contracts, tests, docs, diff size and CI.
---

# PR checklist

Go through every section and report what passes and what needs work. With a human in the loop, end with a short
list of follow-ups and open decisions.

To review an open PR, read every comment, including inline review comments:

```sh
gh pr view <pr> --comments
gh api --paginate "repos/llm-d-incubation/llm-d-resiliency-operator/pulls/<pr>/comments"
gh api --paginate "repos/llm-d-incubation/llm-d-resiliency-operator/pulls/<pr>/reviews"
```

## 1. Design fit

- The controller stays engine- and workload-neutral: `internal/controller/` imports no implementation and names
  no engine or workload.
- Engine logic is in the engine package, workload logic in the workload package. The policy stays pure.
- The safety rules hold: removal only of Terminal members, journal saved before acting, one reconfig in flight,
  no action on a group IRO cannot see, workload calls by UID and repeatable.
- A contract change (an interface or object in `internal/engine`, `internal/workload`, `internal/journal` or the
  controller) is called out in the description, and the matching `docs/components/` page is updated in the docs PR.

## 2. Tests

- New behavior has a test that fails without the change.
- A fault-handling change has a test per outcome: the in-place path and the reset path.
- `make test` passes with `-race`.

## 3. Diff

- One concern. No unrelated renames, reformatting or refactors.
- No new comments that restate the code. The license header and a one-line package comment per package.
- No duplicated constants: grep a new constant's value before adding it.
- No debug output, no leftover files.

## 4. Docs

- Behavior that users see is in the docs, in its own PR.
- Engine-neutral pages name no engine or workload.

## 5. CI and git

- `make build`, `make test`, `make lint` pass locally. Python changes pass `ruff check .` and `ruff format --check .`.
- Every commit is signed and signed off, with no AI or tool mentions.
- The branch is up to date with `main` by merge, not rebase.
- The description follows `.github/PULL_REQUEST_TEMPLATE.md`, is short, and lists the tests that ran.

## 6. Review feedback

For a re-review: every comment has a reply or a change, the description still matches the code, and resolved
threads are not reopened by the new diff.
