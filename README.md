# agent-handoff — Git for agents

[![build](https://github.com/adeelahmad/package/actions/workflows/build.yml/badge.svg)](https://github.com/adeelahmad/package/actions/workflows/build.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Git for agents.** A portable context builder: package any body of work — a design
session, a legal matter, a research plan, anything — into a **versioned, self-describing
agent-to-agent handoff package**, with a git-style history that agents *query* instead of
browse. Built for agents; usable by people. Ships as a **Claude Code** / **OpenCode**
plugin, a static binary, and a one-liner any agent can follow.

## The one-liner

Paste this into **any** agent with internet access — claude.ai, chatgpt.com, Claude
Code, Codex, Gemini, anything — and it will package the current conversation:

```
Fetch https://raw.githubusercontent.com/adeelahmad/package/master/PROMPT.md and follow it exactly to package this conversation into an agent-handoff package.
```

[`PROMPT.md`](PROMPT.md) is self-contained and degrades gracefully: agents with a shell
install the binary and build the sealed, versioned archive; agents that can only write
files or chat emit the same portable tree plus `data.json`, which any binary-holding
agent can seal later — no capability is left out.

To install just the binary on any machine:

```
curl -fsSL https://raw.githubusercontent.com/adeelahmad/package/master/install.sh | sh
```

(Prebuilt release binary when available, otherwise builds from source with Go;
installs to `~/.local/bin`, override with `HANDOFF_INSTALL_DIR`.)

## Why

When one agent session hands work to the next, three things usually go wrong: the
receiving agent re-derives decisions that were already settled, works tasks in the wrong
order, and trips over stale copies of superseded files. A handoff package makes all
three structurally impossible:

- **Never re-establish settled facts.** SETTLED (inputs) and OPEN (resolve or ask) are
  explicit and enforced disjoint.
- **Ordered work.** A dependency-annotated task list; agents complete a unit, return.
- **One visible version, full history.** The working tree is only the current version.
  Every prior version is sealed in `.handoff/` — a content-addressed store where each
  unique file is kept once — and reached only through the binary:
  `handoff history PATH`, `show PATH --at v2`, `diff PATH`, `extract DIR DEST --at v1`.
  No stale folders for an agent to trip over.
- **Every version says why.** `commit -m` is mandatory; empty versions are refused;
  `pack` refuses a tree with uncommitted changes, so a package's history always matches
  its contents; `verify` re-hashes everything.
- **Readable before extraction.** `handoff inspect pkg.tar.gz` returns the intake, the
  manifest and the complete history straight from the archive stream.
- **Domain-neutral.** Workstreams + plan documents. `--profile software` is one preset.
- **Self-contained option.** `pack --bundle-bin bin/` ships the binaries for every
  platform inside the package, under `.handoff/bin/`.

## Install

### As a Claude Code plugin

This repository is its own plugin marketplace. In Claude Code:

```
/plugin marketplace add adeelahmad/package
/plugin install agent-handoff@agent-handoff-marketplace
```

That gives you two slash commands backed by two skills:

| Command | Skill | What it does |
|---|---|---|
| `/handoff-package [out.tar.gz] [--profile software] [--bundle-bin]` | `handoff-packager` | Distills the current conversation into a versioned handoff package |
| `/handoff-unpack [package.tar.gz \| unpacked-dir]` | `handoff-unpacker` | Inspects, unpacks and refines a package; queries its history via the binary |

The unpacker skill is also **bundled into every package** it produces (under
`.skills/unpacker/`), so a package can be received by an agent that never installed
the plugin.

### OpenCode

`opencode/plugin.json` is a best-effort descriptor for OpenCode pointing at the same
commands, skills and binary. It has not been verified against OpenCode's published
plugin schema; the Claude Code manifest is the verified one.

### From source

```
make build      # cross-compile linux/darwin/windows (amd64, arm64) into bin/
make test       # go vet + unit tests + selfcheck
make install    # copy bin/handoff to ~/.local/bin
```

Pure Go standard library — one static binary per platform, no runtime dependencies.
CI (`.github/workflows/build.yml`) runs `make test` and `make build` on every push and
uploads the binaries as an artifact.

## Use

Author side, one shot:

```
handoff package --data data.json -m "first cut" --out pkg.tar.gz
```

Receiving side:

```
handoff inspect pkg.tar.gz --human        # intake, manifest, full history — no extraction
tar -xzf pkg.tar.gz && cat AGENTS.md      # read-first: intake, SETTLED, OPEN, TASK ORDER
handoff history <workstream>/plan.md      # then query history — never browse .handoff/
```

`data.json` is the seam between agent and tool: what goes **in** is the agent's judgment
(intake, settled, open, tasks, workstreams, invariants, facts, overview); how it is laid
out, validated, versioned and shipped is the binary's, deterministically. The binary
rejects structural mistakes it can see: missing intake or workstreams, a workstream with
no plan document, an item listed as both settled and open, an unknown profile.

## The `handoff` binary

```
usage: handoff [-C DIR] <command> [options]

  init      [DIR]                                start a store (.handoff/) in DIR (default: .)
  scaffold  --data FILE [--profile P] [--unpacker-skill DIR]
                                                 render AGENTS.md, docs/, manifest from a data file
  commit    -m "why"   [--allow-empty]           record the working tree as the next version (comment MANDATORY)
  status                                         what changed since the last version
  history   [PATH]     [--human]                 versions that changed PATH (file or dir); newest first
  show      PATH       [--at REF]                print one file as it was at REF (default: latest)
  ls        [PATH]     [--at REF] [--human]      list files under PATH at REF
  extract   PATH DEST  [--at REF]                copy a file / dir / "." out of REF, standalone
  diff      [PATH]     [--from REF] [--to REF]   what changed between versions (file: unified diff)
  verify                                         re-hash every object, check every version
  pack      --out FILE [--bundle-bin DIR] [--allow-dirty]
                                                 write the distributable .tar.gz (refuses a dirty tree)
  package   --data FILE -m "why" --out FILE [--profile P] [--unpacker-skill DIR] [--bundle-bin DIR]
                                                 init (if needed) + scaffold + commit + pack, in one go
  inspect   FILE.tar.gz                          read metadata + full history WITHOUT extracting
  selfcheck | version | help
```

Refs: `latest` | `HEAD` | `vN` | commit id (≥4-char prefix) | any of these `~N` (N back).
Query output is JSON by default; `--human` for people. Flags may go before or after paths.

## Anatomy of a package

What an agent sees after `tar -xzf`:

```
<package-name>/
  AGENTS.md                      # read-first: intake, SETTLED, OPEN, TASK ORDER, invariants
  handoff.manifest.json          # the same intake, machine-readable; read_order
  docs/ESTABLISHED-FACTS.md      # facts as INPUTS, with their WHY
  docs/overview.md               # the shape of the work (thin by default)
  <workstream-a>/…/<plan-doc>    # one directory per workstream
  <workstream-b>/…/<plan-doc>
  .skills/unpacker/SKILL.md      # the unpacker skill, bundled so the package self-reads
  .handoff/                      # the sealed version store — NEVER browsed, only queried
```

The store is git-style and content-addressed: objects by sha256 (each unique file
stored once, so unchanged files cost zero bytes in later versions), an append-only log,
and commit ids that are the sha256 of their own canonical content — `handoff verify`
re-derives everything, so tampering or corruption is always detected. The archive's PAX
global header carries the intake and version metadata, which is what lets `inspect`
answer without extracting.

The complete format specification lives in
[`docs/PACKAGE-FORMAT.md`](docs/PACKAGE-FORMAT.md) — the shared source of truth that
both skills conform to.

## Repository layout

```
.claude-plugin/plugin.json      Claude Code manifest       commands/   /handoff-package, /handoff-unpack
.claude-plugin/marketplace.json Marketplace entry          skills/     packager + unpacker (unpacker is bundled into every package)
opencode/plugin.json            OpenCode descriptor        tool/       Go source (stdlib only), embedded templates, tests
docs/PACKAGE-FORMAT.md          the format spec            bin/        cross-compiled binaries (make build)
```

## License

[MIT](LICENSE)
