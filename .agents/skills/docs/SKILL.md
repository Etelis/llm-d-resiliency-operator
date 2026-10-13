---
name: docs
description: Write or change IRO's docs, figures, interactive steppers or failure pages, and build the docs site. Use for any change under docs/ or website/.
---

# Docs

## Where things go

| Path | Holds |
|---|---|
| `docs/README.md` | the architecture overview: the site's landing page |
| `docs/components/` | one page per component; engines and workloads each have a general page and one per implementation |
| `docs/failures/` | one page per failure IRO handles |
| `docs/scenarios/*.json` | the data behind each interactive stepper |
| `docs/figures/` | SVG figures and `figures.py`, which draws them |
| `website/` | the Docusaurus site; `sidebars.js` sets the order |

## Rules

- General pages never name an engine or workload. Only an implementation's own page does.
- A component page reads top to bottom: what it does, what it talks to, its subjects in the order they happen,
  the API reference last. Rules sit in the section they govern.
- Figures over text. A state diagram shows states and transitions only.
- One fact, one place. Link to it instead of repeating it.
- Plain statements; see "Writing" in `AGENTS.md`.

## Figures

Edit `docs/figures/figures.py` and run `python3 docs/figures/figures.py`. Never edit an SVG by hand. The script
must pass `ruff check` and `ruff format --check`.

## Steppers

A stepper is a JSON file in `docs/scenarios/`, shown on a page by one line that GitHub hides:

```markdown
[example my-scenario]: ../scenarios/my-scenario.json
```

On the site it replaces any figure or table right above it, so put the static version there for GitHub readers.
The JSON lists the Pods once, then the steps; each step names only what changed. The field reference is at the
end of `docs/failures/index.md`. A component page shows its own concept; a failure page shows the failure.

## A new failure

Add `docs/failures/<name>.md` (summary; what IRO sees, does and ends with; the rules involved), its scenario JSON,
a row in the catalog in `docs/failures/index.md`, and the page in `website/sidebars.js`. Match the contract: read
`docs/components/controller.md` and the code before describing what IRO does.

## Build

```sh
cd website && npm ci && npm run build
```

Broken links fail the build; broken anchors print a warning to fix. `npm run serve` serves the build locally.
