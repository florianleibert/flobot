---
name: pipeline
description: Turn a product brief into an epic, break it into independent tasks, implement each behind review, and merge.
---

# Pipeline

Take a written product brief and drive it to merged code through fixed, reviewable
stages. This is a workflow, not a persona: follow the stages in order, keep each
artifact small, and stop for the operator at every gate. It uses the ordinary
loop and tools — there is no separate engine, daemon, or task runner.

## When to use

The operator hands you a PRD, a feature brief, or a rough "build X" and wants it
carried from intent to a merged change with an auditable trail. If the request is
a single edit, skip this skill and just make the edit.

## Inputs

- A product brief in the workspace (for example `docs/prd.md`) or pasted inline.
- The repository you are already in. Never edit another checkout.

## Stages

1. **Frame the epic.** Read the brief. Write `docs/epic.md`: the one-sentence
   outcome, in-scope bullets, explicit non-goals, and the acceptance checks that
   decide "done". Do not invent scope the brief does not imply. Show it and wait
   for the operator to accept before decomposing.

2. **Decompose into tasks.** Split the epic into independent tasks, each scoped to
   specific files with **no overlapping edits** between tasks. Give each a stable
   id (`T-1`, `T-2`, …), the files it owns, its dependencies, and its own
   acceptance check. Record them in `docs/tasks.md`. Order them by dependency; note
   which are parallel-safe (disjoint files) and which must be sequential.

3. **Implement one task at a time.** For each task in dependency order: switch to
   PLAN, produce a short plan the operator can approve, then run it in CODE behind
   the permission chain. Touch only the files that task owns. Write the test first
   or alongside; a task is not done until its acceptance check is green. Commit on
   a feature branch with a message naming the task id — never push to a default
   branch.

4. **Review before merge.** For each finished task, review the diff for
   correctness, tests, and whether it stayed inside its files. Report findings with
   `file:line` evidence. Fix what you find, or hand the finding back with a clear
   explanation. Do not mark a task merge-ready on "looks fine".

5. **Integrate and verify.** When the tasks that make up the epic are green,
   reconcile them, run the full test suite, and confirm every acceptance check in
   the epic holds. Summarize what landed, the test result, and anything left out
   and why.

## Rules

- Each stage produces one small artifact the operator can read and correct.
- Never let two in-flight tasks edit the same file.
- A failing check is a stop, not a footnote: report it with the output.
- No task is "complete" until its tests pass and its diff has been reviewed.
- Parallelism, when used, is separate worktrees or processes — one task per tree.

## Output

At the end: the epic, the task list with per-task status, the merged (or
ready-to-merge) branches, the full-suite test result, and a plain summary that
stands on its own.
