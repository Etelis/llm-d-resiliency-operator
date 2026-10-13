---
name: commit
description: Make a git commit in this repo. Use whenever a change is committed, amended or reworded.
---

# Commit

1. Run `make fmt`, then `make build`, `make test` and `make lint` when Go changed. Fix what they report.
2. Check spelling on the changed files with `typos`, if it is installed.
3. Stage only the files of this change. Never commit build output, local settings or `.claude/worktrees/`.
4. Commit signed and signed off:

   ```sh
   git commit -S -s -m "Subject in the imperative"
   ```

   The signature must be one GitHub can verify (an SSH or GPG key on the author's account): CI rejects unverified
   commits.
5. Message: an imperative subject of about 72 characters, and a short body on why when the subject is not enough.
   No AI, agent or tool mentions, no co-author trailers, no "generated with" lines.

Never `--no-verify`, never amend or rewrite a commit that is already pushed, never force-push.
