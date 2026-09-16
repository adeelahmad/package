---
description: Package this conversation into a versioned agent-handoff package (any domain)
argument-hint: [output.tar.gz] [--profile software] [--bundle-bin]
---

Invoke the **handoff-packager** skill. Harvest what this conversation established (as
inputs, never questions), separate SETTLED from OPEN, order the TASKS with dependencies,
split the work into workstreams each carrying a plan document, write `data.json`, then
build with the binary — never by hand:

`${CLAUDE_PLUGIN_ROOT}/bin/handoff package --data data.json -m "<why this version exists>" --out $1 --unpacker-skill ${CLAUDE_PLUGIN_ROOT}/skills/handoff-unpacker $2 $3`

Thin by default; embed facts/overview only if the user asks. If the package directory
already has a `.handoff/` store, do NOT re-init: edit, `handoff commit -m "…"`, `handoff pack`.
