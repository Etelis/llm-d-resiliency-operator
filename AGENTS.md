# AGENTS.md

llm-d Resiliency Operator (IRO). A Go Kubernetes operator that keeps a multi-node inference engine serving
through faults: it removes failed engine members in place, brings their Pods back, and recreates the whole group
only when nothing smaller can work. Part of the llm-d project. Review time is shared, so keep changes small and
discuss larger ones in an issue first.

`make help` lists the targets. `make build`, `make test` and `make lint` are the gate for every change.

## Agent operating rules

**Allowed.** Read the code, the docs and GitHub state. Edit code and docs. Run `make` targets and `go test`.

**Ask first.** Pushing to any branch, opening a PR, rewriting pushed history, edits under `.github/` or to
`OWNERS`, dependency upgrades, anything that touches a cluster. Propose the change and the reason in one message,
and wait.

**Never, without explicit approval for that one action.** Comments, reviews, approvals, labels or state changes
on GitHub, and posts anywhere else under the user's name. Draft them as text for the user instead.

## Layout

| Path | What it holds |
|---|---|
| `cmd/main.go` | the manager: flags, leader election, registers engines and workloads |
| `internal/controller/enginefault/` | the controller: one loop per engine group. Engine- and workload-neutral. |
| `internal/engine/` | the engine contract (`Driver`, `Policy`); `internal/engine/<name>/` implements one engine |
| `internal/workload/` | the workload contract; `internal/workload/<name>/` implements one workload type |
| `internal/journal/` | the journal contract; implementations under it |
| `docs/` | design docs and the docs site content; `website/` is the Docusaurus site |

## How IRO is built

- **The controller knows no engine and no workload.** Never import an engine or workload implementation from
  `internal/controller/`, and never put engine names, ranks, ports or status strings there. What is specific to
  one engine lives in its engine package; what is specific to one workload type lives in its workload package.
- **The contracts are the interfaces.** `internal/engine`, `internal/workload`, `internal/journal` and the
  controller's objects define them; `docs/components/` explains them. A change to a contract changes both.
- **Safety rules are not optional.** A member is removed only when it is proven gone (Terminal). The journal is
  saved before every action. One reconfig is in flight at a time. IRO never acts on a group it cannot see. The
  full list is in `docs/components/controller.md` (safety checks and guarantees).
- **The policy is pure.** No I/O, no clock, no globals: the same observation gives the same plan on any leader.
- **Workload calls name Pods by UID and can be repeated.** A late call must never touch a new Pod.

## Working in the codebase

- State your reading of the task before coding. If it can be read two ways, ask.
- Define success as something you can check: a failing test that the change makes pass.
- Before changing or adding a component, read the closest existing one and follow its structure, naming and tests.
- Tests in the same package describe the contract. Read them before changing behavior.
- Check behavior against the code, not against names or memory.
- Do not say a change is done until `make build`, `make test` and `make lint` ran and passed, and you saw the output.
- When something goes sideways, stop, restate what you know, and propose a new plan before continuing.

## Code style

- Standard Go. `make fmt` and `make lint` are authoritative.
- Minimal comments: the license header and a one-line package comment. A comment only where the reason is not
  obvious from the code. Explanations belong in `docs/`.
- Describe the current state. No "previously", "now", "renamed from", "this PR" or other references to history.
- State each fact once, in its home. Do not repeat it across code, comments, tables and examples.
- No Unicode symbols unless asked for.

## Docs

- Pages about the controller, engines in general or workloads in general never name a specific engine or
  workload. Only an implementation's own page names it.
- Every component page reads top to bottom: what it does, what it talks to, its subjects in the order they
  happen, then the API reference last.
- Figures over text. Figures are drawn by `docs/figures/figures.py`; interactive steppers come from JSON files in
  `docs/scenarios/`. Never edit a generated SVG by hand.
- The site must build: `cd website && npm ci && npm run build`. Broken links fail the build.
- Design docs go in their own PR, not mixed into a code PR.

## Writing

Plain statements of fact. Before shipping any prose (docs, comments, commit messages, PR descriptions), remove
these, following the llm-d router's list:

| Pattern | Example | Fix |
|---|---|---|
| X, not Y | "the journal is not a log, it is state" | say what it is, once |
| Aphorism or closing line | a paragraph that ends on a quotable line | end on information |
| Triads for rhythm | "no locks, no races, no drift" | one clause |
| Counters | "three things follow" | just say them |
| Intensifiers | genuinely, critically, robust, seamless | name the verified behavior |

## Pull requests

- Smallest correct change. One concern per PR, tracked in an issue for anything non-trivial.
- No renames, reformatting or refactors beyond what the issue needs. Mention unrelated problems; do not fix them.
- Use `.github/PULL_REQUEST_TEMPLATE.md`. Keep the description short and to the point.
- Run the `pr-checklist` skill before opening a PR or asking for another review.

## Git

- Every commit is signed off (`-s`, DCO) and signed (`-S`). CI rejects commits GitHub cannot verify.
- Subject in the imperative, about 72 characters. Short body on why.
- No AI, agent or tool mentions in commits or PRs: no co-author trailers, no "generated with" lines.
- Never force-push. Bring a branch up to date by merging `main` into it.
- Never bypass hooks (`--no-verify`) or signing.

## Skills

Task guides live in `.agents/skills/` (also at `.claude/skills/`):

| Skill | Use when |
|---|---|
| `commit` | making any commit |
| `pr-checklist` | before opening a PR, or after addressing review |
| `add-engine` | adding support for an inference engine |
| `add-workload` | adding support for a workload type |
| `docs` | writing or changing docs, figures or failure pages |
| `ci-fails` | a CI check failed |
