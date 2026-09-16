---
description: Receive, inspect and refine an agent-handoff package; query its history via the binary
argument-hint: [package.tar.gz | unpacked-dir]
---

Invoke the **handoff-unpacker** skill. First `handoff inspect $1 --human` (no
extraction), then `tar -xzf` and read in the manifest's read_order. Treat SETTLED facts
as inputs — do NOT re-establish them. Resolve only OPEN items. Do the TASKS in order,
returning after each. For anything about prior versions use the binary
(`handoff history PATH`, `show PATH --at vN`, `diff PATH`, `extract`) — never look for
old copies in the tree; there are none.
