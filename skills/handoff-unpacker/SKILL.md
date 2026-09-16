---
name: agent-handoff-unpacker
description: >-
  Receive, read and refine an agent-handoff package (an archive or unpacked directory
  containing AGENTS.md, handoff.manifest.json and a sealed .handoff/ store). Use when
  you are handed such a package, or asked to check what changed between versions of
  one. Conforms to docs/PACKAGE-FORMAT.md.
---

# Agent Handoff Unpacker

You are the RECEIVING agent. You have context the packager lacked (real material, live
environment). Your job is to REFINE, then execute — never to execute blindly, and never
to re-establish what the package marks settled.

## Steps
1. **Inspect before extracting**: `handoff inspect <file>.tar.gz --human` — intake,
   profile, head version, full version history with comments.
2. `tar -xzf <file>.tar.gz`; read in the manifest's `read_order`: AGENTS.md,
   docs/ESTABLISHED-FACTS.md, docs/overview.md, then each workstream's plan document.
3. Treat SETTLED and ESTABLISHED-FACTS as **inputs**. Do not re-investigate or re-confirm.
4. Resolve **only** the OPEN items — against the real material you can see, or by asking
   the human where the package says a human answer is needed.
5. Do the TASKS **in order**; return and report after each unit; respect dependencies.
6. Correct / split / merge each workstream's plan document where reality differs from
   the plan. If the `software` profile is in use, hand each refined `stories.md` to the
   planner (e.g. agentic-agile) per workstream; do not hand-author its artifacts.

## History — ask the binary, never browse
The tree you see is the ONE current version. Prior versions are sealed in `.handoff/`.
If `.handoff/bin/` exists, use the binary for your platform from there.
- What changed, and why: `handoff history [PATH]` (JSON), `handoff diff PATH`
- A file as it was: `handoff show PATH --at v2`; a directory: `handoff extract DIR DEST --at v2`
- Trust: `handoff verify`
Never reconstruct history from memory or from the conversation — the store is the record.

## Handing back
`handoff commit -m "<what you changed and why>"` (mandatory comment), then
`handoff pack --out <name>.tar.gz`. If a settled fact is contradicted by the real
material, surface it to the human as a conflict and record it in the commit comment —
do not silently redesign.
