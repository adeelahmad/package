---
name: agent-handoff-packager
description: >-
  Distill the current conversation — about anything, not only software — into a
  versioned, self-describing agent-to-agent handoff package: an AGENTS.md intake, an
  ESTABLISHED-FACTS doc that prevents re-litigation, an overview, and one plan document
  per workstream, committed into a git-style history the receiving agent queries
  through the `handoff` binary. Use when asked to "package this for another agent",
  "hand this off", "make a handoff bundle", "version this handoff", or to prepare work
  for an agent that has context you lack. Conforms to docs/PACKAGE-FORMAT.md.
---

# Agent Handoff Packager

You decide WHAT goes in (judgment). The `handoff` binary decides HOW it is laid out,
validated, versioned and shipped (deterministic). Never hand-roll the layout, the
manifest, the archive, or the history — that is what the binary is for.

## Steps
1. **Harvest what the conversation established** — with its WHY. Phrase as inputs, not
   questions. This is what stops the next agent re-deriving what you already settled.
2. **Separate SETTLED from OPEN.** Settled = decided, do-not-reinvestigate. Open = only
   what genuinely needs the real material or a human answer. Keep them disjoint (the
   binary refuses overlap).
3. **Order the TASKS with explicit dependencies** ("3 depends on 1"). The receiving
   agent completes one unit, returns, then continues.
4. **Split the work into workstreams** — one directory each, by where the work happens.
   Cross-reference anything two workstreams share.
5. **Write one plan document per workstream**: `plan.md` (default) or `stories.md`
   (`--profile software`, five-part intent per story, agentic-agile compatible).
6. **Write `data.json`** — intake, settled, open, tasks, workstreams, invariants; add
   facts/overview ONLY if the user asked for a richer package (thin by default).
7. **Build with the binary**, from the package directory, in one step:
   `handoff package --data data.json -m "<why this version exists>" --out <name>.tar.gz
   --unpacker-skill ${CLAUDE_PLUGIN_ROOT}/skills/handoff-unpacker [--profile software]
   [--bundle-bin ${CLAUDE_PLUGIN_ROOT}/bin]`
   (or the primitives: `init` → `scaffold` → `commit -m` → `pack`).
8. **A later session on the same package**: edit the tree, `handoff commit -m "…"`,
   `handoff pack --out …`. Never copy old versions around — the store keeps them.

## Rules
- The commit comment is mandatory and must say what changed and why. Do not restate the
  label (`v3`) — the binary prefixes it.
- Do NOT bake project specifics into this skill — they live in the package content.
- Do NOT invent facts; only what the conversation established. Real unknowns go OPEN.
- Do NOT bypass a refusal (`--allow-dirty`, `--allow-empty`) without telling the user why.
- Do NOT relax the downstream invariants listed in AGENTS.md.
