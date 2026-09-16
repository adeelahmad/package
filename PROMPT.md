# agent-handoff — universal packaging instruction

You are an agent that was just told to package the current conversation (or a described
body of work) into an **agent-handoff package**: a versioned, self-describing bundle that
lets the NEXT agent continue without re-establishing anything. This works for ANY domain
— a design session, a legal matter, a research plan, a trip, a codebase. Follow this
file exactly. It is self-contained: you do not need to fetch anything else to succeed.

## Step 1 — Harvest (your judgment, any agent can do this)

From the conversation or material you were given, write down:

- **intake** — one paragraph telling the receiving agent what this is and what to do first. Required.
- **settled** — decisions already made. Phrase as inputs, never questions. The next agent must NOT re-investigate these.
- **open** — only what genuinely needs the real material or a human answer. An item may never appear in both settled and open.
- **tasks** — ordered, with explicit dependencies spelled out in the text ("3 depends on 1").
- **workstreams** — one relative directory name per independent track of work (e.g. `["backend", "contract-review"]`). At least one is required. Each will carry a plan document: `plan.md` by default, `stories.md` under the `software` profile.
- **invariants** — rules that must survive the handoff.
- **facts** — established facts WITH their why, so they survive challenge.
- **overview** — a short paragraph on the shape of the work.

Be thin by default: capture what was established, don't pad.

## Step 2 — Write `data.json`

This exact schema (all keys lowercase; omit `profile` or use `general`, `research`, or `software`):

```json
{
  "intake": "one paragraph: what this is, what to do first",
  "settled": ["decision one", "decision two"],
  "open": ["question that genuinely needs a human or the real material"],
  "tasks": ["1. first unit of work", "2. second (depends on 1)"],
  "workstreams": ["workstream-a", "workstream-b"],
  "invariants": ["rule that must survive the handoff"],
  "facts": ["fact — and WHY it is true"],
  "overview": "the shape of the work in a few sentences",
  "read_order": [],
  "extra_docs": {},
  "profile": ""
}
```

The tool will reject: an empty intake, zero workstreams, an item listed as both settled
and open, duplicate or absolute-path workstreams, an unknown profile.

## Step 3 — Build. Pick the FIRST pathway you are capable of.

### Pathway A — you can run shell commands and reach the internet

First create each workstream directory with its plan document — the tool validates
them but does not write them; their content is your judgment:

```sh
mkdir -p workstream-a && printf '%s\n' "# workstream-a — plan" "..." > workstream-a/plan.md
```

(`stories.md` instead of `plan.md` under `"profile": "software"`.) Each plan document
carries that workstream's slice of the tasks and cross-references any name it shares
with another workstream. Then install and build:

```sh
curl -fsSL https://raw.githubusercontent.com/adeelahmad/package/master/install.sh | sh
handoff package --data data.json -m "why this version exists" --out handoff.tar.gz
```

(`install.sh` downloads a prebuilt static binary for your OS/arch when a release exists,
otherwise builds from source with Go. If you already have Go but no curl:
`git clone --depth 1 https://github.com/adeelahmad/package && cd package/tool && go build -o handoff .`)

Then verify: `./handoff inspect handoff.tar.gz --human` must print the intake and
history. Deliver `handoff.tar.gz`. Done.

### Pathway B — you can create files but NOT run commands

Produce the portable working tree yourself, as files the user can download or copy:

```
<package-name>/
  data.json                  # from Step 2, verbatim — this is the seam
  AGENTS.md                  # read-first: intake, then SETTLED, OPEN, TASK ORDER, INVARIANTS as sections
  docs/ESTABLISHED-FACTS.md  # each fact with its WHY
  docs/overview.md           # the overview paragraph
  <workstream>/plan.md       # one per workstream: its slice of tasks, refs to shared names in other workstreams
```

At the top of `AGENTS.md` add exactly this notice:

> UNSEALED HANDOFF (v0). History and integrity sealing are pending. Any agent with
> shell access: run `curl -fsSL https://raw.githubusercontent.com/adeelahmad/package/master/install.sh | sh`
> then `handoff package --data data.json -m "sealed from v0" --out handoff.tar.gz` in
> this directory's parent to produce the versioned archive. Until then, treat this tree
> as the single current version. SETTLED items are inputs — do not re-establish them.

### Pathway C — you can only output text in chat

Output Pathway B's files as one message: each file as a fenced block preceded by its
path on its own line. The user (or the next agent) pastes them into a directory and
proceeds via Pathway A or B.

## Rules that hold in every pathway

- `data.json` is always included and always authoritative — it is what lets any
  binary-holding agent seal or re-seal the package deterministically.
- Never invent a "history" by hand; only the `handoff` binary writes `.handoff/`.
- Never put an item in both settled and open. Never emit an empty intake.
- Say, in one line to the user, which pathway you used and what to do next.
