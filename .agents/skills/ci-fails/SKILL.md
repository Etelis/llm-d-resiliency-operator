---
name: ci-fails
description: Find out why a CI check failed on a PR in this repo and fix it, or re-run it when the failure is not caused by the change.
---

# CI fails

Logs are long: write them to a file and search it, do not read them whole.

```sh
gh pr checks <pr>
gh run view <run-id> --log-failed > /tmp/iro-ci-<run-id>.log
grep -n -E "error|Error|FAIL|--> " /tmp/iro-ci-<run-id>.log | head -50
```

## The checks

| Check | Runs | Reproduce locally |
|---|---|---|
| `lint-and-test` | golangci-lint, then `go test -race` | `make lint-go`, `make test` |
| `python-lint` | `ruff check .` and `ruff format --check .` on any Python file | `ruff check .`, `ruff format --check .` |
| `container-build` | builds the Dockerfile | `make image-build` |
| `signed-commits` | every commit in the PR is verified by GitHub | `git log --show-signature` |
| `remove-lgtm`, `gatekeeper`, Prow | Prow labels and merge rules | |

## Failures that are not the change

- **golangci-lint `config verify` cannot load its schema** (`failing loading ".../golangci.v2.x.jsonschema.json"`):
  a download hiccup. Re-run the failed jobs: `gh run rerun <run-id> --failed`.
- **Prow cannot apply a label** (`the label(s) ... cannot be applied because the repository doesn't have them`):
  the label is missing from the repository. Ask a maintainer to create it, then re-run.

## Failures that are

- **lint-and-test**: fix the lint findings or the failing test; never disable a linter for one line without a reason
  in the review.
- **python-lint**: run `ruff format` and fix what `ruff check` reports. Keep Python runnable on the versions the
  repo supports.
- **signed-commits**: the commit was not signed with a key on the author's GitHub account. Add a new signed commit;
  do not force-push to replace the old one without the user's approval.

After a fix, push a new commit and watch the checks until none are pending.
