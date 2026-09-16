# Agent Handoff Package — format spec (shared source of truth, v2)

A handoff package carries a body of work — about ANYTHING, not only software — from
one agent session to the next, so the receiving agent never re-establishes what was
already decided, works the tasks in order, and can walk the full history of any file
without ever seeing stale copies. Both skills (packager, unpacker) conform to THIS spec.

## Vocabulary (domain-neutral)
- **Workstream** — any independent track of work (a system, a legal matter, a study
  arm, a chapter). One directory each. Cross-reference anything two workstreams share.
- **Plan document** — the file every workstream must carry. Its NAME comes from the
  profile: `plan.md` by default (also `general`, `research`); `stories.md` for the
  `software` profile (agentic-agile compatible). Software is a preset, not an assumption.
- **Version** — one committed snapshot of the working tree, labelled `v1, v2, …`, with a
  MANDATORY comment saying why it exists.

## Layout of the working tree (what an agent sees after `tar -xzf`)
```
<package-name>/
  AGENTS.md                      # read-first: intake, SETTLED, OPEN, TASK ORDER, invariants
  handoff.manifest.json          # the same intake, machine-readable; read_order
  docs/ESTABLISHED-FACTS.md      # facts as INPUTS, with their WHY (may be intentionally thin)
  docs/overview.md               # the shape of the work (thin by default)
  <workstream-a>/…/<plan-doc>    # one directory per workstream
  <workstream-b>/…/<plan-doc>
  .skills/unpacker/SKILL.md      # the unpacker skill, bundled so the package self-reads
  .handoff/                      # the sealed version store — NEVER browsed, only queried
```
There is exactly ONE visible version: the current one. No `lineage/`, no `v2/` folders,
no `.bak` files. That is what keeps agents from acting on superseded state.

## The store (`.handoff/`) — history you query, never browse
A git-style content-addressed store, read and written only by the `handoff` binary:
```
.handoff/FORMAT            "agent-handoff-store 1"
.handoff/HEAD              id of the latest version
.handoff/log               append-only, oldest first:  <id>\t<label>\t<timestamp>
.handoff/objects/aa/bb…    file contents by sha256 — every unique file stored ONCE
.handoff/commits/<id>.json {label, parent, comment, timestamp, tree{path→hash,size,mode}}
.handoff/bin/              (optional) the handoff binaries, so the package is self-contained
```
- Dedup is by file content hash: unchanged files cost zero bytes in every later version.
- A commit id is the sha256 of its own canonical content; `handoff verify` re-hashes
  every object and re-derives every id, so tampering or corruption is always detected.
- The store never contains itself, `.git`, or OS litter.

## The binary — what agents point at
```
handoff history [PATH]              versions that changed PATH (file or dir), newest first — JSON
handoff show PATH --at REF          one file exactly as it was at REF
handoff ls [PATH] --at REF          the files under PATH at REF
handoff diff [PATH] --from A --to B unified diff for a file; JSON tree diff otherwise
handoff extract PATH DEST --at REF  file / directory / "." out of any version, standalone
handoff status                      changes since the latest version
handoff verify                      integrity of the whole store
handoff inspect FILE.tar.gz         metadata + full history WITHOUT extracting
```
Refs: `latest` | `HEAD` | `vN` | commit id (≥4-char prefix) | any of these `~N` (N back).
Query output is JSON by default; `--human` for people. Flags may go before or after paths.

## Writing a version
```
handoff init                        once, in the package directory
handoff scaffold --data data.json   renders AGENTS.md, docs/, manifest; validates workstreams
handoff commit -m "why"             MANDATORY comment; refuses an empty version
handoff pack --out NAME.tar.gz      refuses uncommitted changes → history always matches contents
handoff package --data … -m … --out …   the four steps in one
```
`data.json` is the seam between the two pathways: what goes IN is the agent's judgment
(intake, settled, open, tasks, workstreams, invariants, facts, overview); how it is laid
out, validated, versioned and shipped is the binary's, deterministically. The binary
rejects structural mistakes it CAN see: missing intake/workstreams, a workstream with
no plan document, an item listed as both settled and open, an unknown profile.

## Archive-level metadata (visible WITHOUT extracting)
The tar's PAX global header carries: `comment` (intake + head version comment),
`HANDOFF.format`, `HANDOFF.head`, `HANDOFF.label`, `HANDOFF.versions`, `HANDOFF.profile`.
`handoff inspect` also recovers the manifest and the complete version history from the
archive stream, so a reader knows everything before deciding to extract.

## Invariants of a good package
- SETTLED and OPEN are explicit and disjoint (the tool enforces disjointness).
- TASKS are ordered and dependency-annotated; an agent returns after each unit.
- Facts carry their WHY, so they survive challenge without re-litigation.
- Work is split by workstream; shared names are cross-referenced in each.
- Thin by default: embed richer detail only when asked.
- Domain-neutral structure; specifics live in content, never in the skills or the tool.
