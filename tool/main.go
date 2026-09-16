// handoff — versioned, domain-neutral agent-to-agent handoff packages.
//
// Git-like verbs, built for agents: point at a file or directory and get its complete
// history; read or extract any single file, directory, or whole version — WITHOUT
// unpacking anything and without stale copies ever appearing in the working tree.
// Query output is JSON by default (the consumer is an agent); --human for people.
//
// Pure Go stdlib. One static binary per platform, no runtime dependencies.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const toolVersion = "2.0.0"

const usage = `handoff ` + toolVersion + ` — versioned agent handoff packages (git-like, agent-first)

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

refs: latest | HEAD | vN | <commit id or ≥4-char prefix> | any of these ~N (N versions back)
`

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "handoff: "+format+"\n", a...)
	os.Exit(1)
}

func emitJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	os.Stdout.Write(append(b, '\n'))
}

func main() {
	args := os.Args[1:]
	dir := "."
	if len(args) >= 2 && args[0] == "-C" {
		dir, args = args[1], args[2:]
	}
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "init":
		cmdInit(dir, rest)
	case "scaffold":
		cmdScaffold(dir, rest)
	case "commit":
		cmdCommit(dir, rest)
	case "status":
		cmdStatus(dir, rest)
	case "history", "log":
		cmdHistory(dir, rest)
	case "show", "cat":
		cmdShow(dir, rest)
	case "ls":
		cmdLs(dir, rest)
	case "extract", "restore":
		cmdExtract(dir, rest)
	case "diff":
		cmdDiff(dir, rest)
	case "verify", "fsck":
		cmdVerify(dir, rest)
	case "pack":
		cmdPack(dir, rest)
	case "package":
		cmdPackage(dir, rest)
	case "inspect":
		cmdInspect(rest)
	case "selfcheck":
		if _, err := loadTemplates(); err != nil {
			die("template load: %v", err)
		}
		fmt.Println("handoff: SELF-CHECK PASS")
	case "version", "--version", "-v":
		fmt.Println("handoff " + toolVersion)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "handoff: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: handoff %s\n", name)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, "\n"+usage)
	}
	return fs
}

// parseArgs accepts flags before, between, or after positionals (git-style), so
// `handoff show PATH --at v2` and `handoff show --at v2 PATH` both work. Returns positionals.
func parseArgs(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				flags = append(flags, a)
				continue
			}
			f := fs.Lookup(name)
			if f == nil {
				fs.Parse([]string{a}) // standard "flag provided but not defined" error + exit
			}
			flags = append(flags, a)
			if bv, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bv.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	fs.Parse(flags)
	return pos
}

func mustStore(dir string) *store {
	s, err := findStore(dir)
	if err != nil {
		die("%v", err)
	}
	return s
}

// ---------- commands ----------

func cmdInit(dir string, args []string) {
	fs := newFlags("init [DIR]")
	pos := parseArgs(fs, args)
	if len(pos) > 0 {
		dir = filepath.Join(dir, pos[0])
	}
	s, err := initStore(dir)
	if err != nil {
		die("init: %v", err)
	}
	emitJSON(map[string]string{"initialized": s.root, "store": s.dir, "format": storeFormat})
}

func cmdScaffold(dir string, args []string) {
	fs := newFlags("scaffold --data FILE")
	data := fs.String("data", "", "JSON data file (intake + workstreams required; the rest opt-in)")
	profile := fs.String("profile", "", "vocabulary preset: general | research | software (default: generic)")
	skill := fs.String("unpacker-skill", "", "directory of the unpacker SKILL to bundle at .skills/unpacker/")
	_ = parseArgs(fs, args)
	if *data == "" {
		die("scaffold: --data is required")
	}
	s := mustStore(dir)
	m, err := scaffold(s.root, *data, *profile, *skill)
	if err != nil {
		die("scaffold: %v", err)
	}
	emitJSON(map[string]any{"scaffolded": s.root, "profile": m.Profile, "plan_doc": m.PlanDoc,
		"read_order": m.ReadOrder, "next": "handoff commit -m \"...\""})
}

func cmdCommit(dir string, args []string) {
	fs := newFlags("commit -m \"why this version exists\"")
	msg := fs.String("m", "", "comment (MANDATORY): why this version exists / what changed")
	allowEmpty := fs.Bool("allow-empty", false, "record a version even if nothing changed")
	_ = parseArgs(fs, args)
	s := mustStore(dir)
	c, err := s.commitTree(*msg, *allowEmpty, time.Now())
	if err != nil {
		die("commit: %v", err)
	}
	parentTree := map[string]entry{}
	if c.Parent != "" {
		if p, err := s.loadCommit(c.Parent); err == nil {
			parentTree = p.Tree
		}
	}
	d := diffTrees(parentTree, c.Tree, ".")
	emitJSON(map[string]any{"label": c.Label, "id": c.ID, "timestamp": c.Timestamp, "comment": c.Comment,
		"files": len(c.Tree), "added": d.Added, "modified": d.Modified, "deleted": d.Deleted,
		"objects_in_store": s.countObjects()})
}

func cmdStatus(dir string, args []string) {
	fs := newFlags("status")
	human := fs.Bool("human", false, "plain-text output")
	_ = parseArgs(fs, args)
	s := mustStore(dir)
	r, err := s.status()
	if err != nil {
		die("status: %v", err)
	}
	if !*human {
		emitJSON(r)
		return
	}
	if r.Label == "" {
		fmt.Println("no versions yet")
	} else {
		fmt.Printf("at %s (%s), %d versions\n", r.Label, short(r.Head), r.Versions)
	}
	if r.Clean {
		fmt.Println("clean — working tree matches the last version")
		return
	}
	for _, p := range r.Added {
		fmt.Println("  added     " + p)
	}
	for _, p := range r.Modified {
		fmt.Println("  modified  " + p)
	}
	for _, p := range r.Deleted {
		fmt.Println("  deleted   " + p)
	}
}

func cmdHistory(dir string, args []string) {
	fs := newFlags("history [PATH]")
	human := fs.Bool("human", false, "plain-text output")
	pos := parseArgs(fs, args)
	s := mustStore(dir)
	q, err := s.normPath(optArg(pos, 0))
	if err != nil {
		die("history: %v", err)
	}
	h, err := s.history(q)
	if err != nil {
		die("history: %v", err)
	}
	if !*human {
		emitJSON(map[string]any{"path": q, "versions": h})
		return
	}
	if len(h) == 0 {
		fmt.Printf("no version has touched %s\n", q)
		return
	}
	for _, e := range h {
		change := e.Change
		if change == "" {
			change = fmt.Sprintf("+%d ~%d -%d", len(e.Added), len(e.Modified), len(e.Deleted))
		}
		fmt.Printf("%-4s %s  %s  %-12s %s\n", e.Label, short(e.ID), e.Timestamp, change, firstLine(e.Comment))
	}
}

func cmdShow(dir string, args []string) {
	fs := newFlags("show PATH [--at REF]")
	at := fs.String("at", "latest", "version to read")
	pos := parseArgs(fs, args)
	if len(pos) != 1 {
		die("show: exactly one PATH is required")
	}
	s := mustStore(dir)
	q, err := s.normPath(optArg(pos, 0))
	if err != nil {
		die("show: %v", err)
	}
	c, err := s.resolve(*at)
	if err != nil {
		die("show: %v", err)
	}
	e, ok := c.Tree[q]
	if !ok {
		for p := range c.Tree {
			if pathMatches(p, q) {
				die("show: %s is a directory at %s — use: handoff ls %s --at %s", q, c.Label, q, c.Label)
			}
		}
		die("show: %s does not exist at %s", q, c.Label)
	}
	b, err := s.readObject(e.Hash)
	if err != nil {
		die("show: %v", err)
	}
	os.Stdout.Write(b)
}

func cmdLs(dir string, args []string) {
	fs := newFlags("ls [PATH] [--at REF]")
	at := fs.String("at", "latest", "version to list")
	human := fs.Bool("human", false, "plain-text output")
	pos := parseArgs(fs, args)
	s := mustStore(dir)
	q, err := s.normPath(optArg(pos, 0))
	if err != nil {
		die("ls: %v", err)
	}
	c, err := s.resolve(*at)
	if err != nil {
		die("ls: %v", err)
	}
	type row struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
		Hash string `json:"hash"`
	}
	var rows []row
	for p, e := range c.Tree {
		if pathMatches(p, q) {
			rows = append(rows, row{p, e.Size, e.Hash})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	if !*human {
		if rows == nil {
			rows = []row{}
		}
		emitJSON(map[string]any{"version": c.Label, "id": c.ID, "path": q, "files": rows})
		return
	}
	for _, r := range rows {
		fmt.Printf("%8d  %s  %s\n", r.Size, short(r.Hash), r.Path)
	}
}

func cmdExtract(dir string, args []string) {
	fs := newFlags("extract PATH DEST [--at REF]")
	at := fs.String("at", "latest", "version to extract from")
	pos := parseArgs(fs, args)
	if len(pos) != 2 {
		die("extract: PATH and DEST are required (PATH may be a file, a directory, or \".\")")
	}
	s := mustStore(dir)
	q, err := s.normPath(optArg(pos, 0))
	if err != nil {
		die("extract: %v", err)
	}
	c, err := s.resolve(*at)
	if err != nil {
		die("extract: %v", err)
	}
	written, err := s.extract(c, q, pos[1])
	if err != nil {
		die("extract: %v", err)
	}
	emitJSON(map[string]any{"version": c.Label, "id": c.ID, "path": q, "written": written})
}

func cmdDiff(dir string, args []string) {
	fs := newFlags("diff [PATH] [--from REF] [--to REF]")
	from := fs.String("from", "", "older version (default: the version before --to)")
	to := fs.String("to", "latest", "newer version")
	pos := parseArgs(fs, args)
	s := mustStore(dir)
	q, err := s.normPath(optArg(pos, 0))
	if err != nil {
		die("diff: %v", err)
	}
	toC, err := s.resolve(*to)
	if err != nil {
		die("diff: %v", err)
	}
	var fromC *commit
	if *from != "" {
		if fromC, err = s.resolve(*from); err != nil {
			die("diff: %v", err)
		}
	} else if toC.Parent != "" {
		if fromC, err = s.loadCommit(toC.Parent); err != nil {
			die("diff: %v", err)
		}
	}
	fromTree, fromLabel := map[string]entry{}, "(nothing)"
	if fromC != nil {
		fromTree, fromLabel = fromC.Tree, fromC.Label
	}
	fe, fok := fromTree[q]
	te, tok := toC.Tree[q]
	if q != "." && (fok || tok) && !isDirIn(fromTree, q) && !isDirIn(toC.Tree, q) {
		var a, b []byte
		if fok {
			if a, err = s.readObject(fe.Hash); err != nil {
				die("diff: %v", err)
			}
		}
		if tok {
			if b, err = s.readObject(te.Hash); err != nil {
				die("diff: %v", err)
			}
		}
		os.Stdout.WriteString(unifiedDiff(q+"@"+fromLabel, q+"@"+toC.Label, a, b))
		return
	}
	d := diffTrees(fromTree, toC.Tree, q)
	if q != "." && d.empty() && !isDirIn(toC.Tree, q) && !isDirIn(fromTree, q) {
		die("diff: %s does not exist at %s or %s", q, fromLabel, toC.Label)
	}
	emitJSON(map[string]any{"path": q, "from": fromLabel, "to": toC.Label,
		"added": d.Added, "modified": d.Modified, "deleted": d.Deleted})
}

func isDirIn(tree map[string]entry, q string) bool {
	for p := range tree {
		if strings.HasPrefix(p, q+"/") {
			return true
		}
	}
	return false
}

func cmdVerify(dir string, args []string) {
	fs := newFlags("verify")
	_ = parseArgs(fs, args)
	s := mustStore(dir)
	r := s.verify()
	emitJSON(r)
	if !r.OK {
		os.Exit(1)
	}
}

func packInto(s *store, out, bundleBin string, allowDirty bool) map[string]any {
	st, err := s.status()
	if err != nil {
		die("pack: %v", err)
	}
	if st.Head == "" {
		die("pack: no versions yet — commit first: handoff commit -m \"...\"")
	}
	if !st.Clean && !allowDirty {
		die("pack: the working tree has uncommitted changes (+%d ~%d -%d) — commit them so the package's history matches its contents, or pass --allow-dirty",
			len(st.Added), len(st.Modified), len(st.Deleted))
	}
	c, err := s.loadCommit(st.Head)
	if err != nil {
		die("pack: %v", err)
	}
	comment := c.Comment
	profile := ""
	if m := readManifest(s.root); m != nil {
		comment = m.Intake + "\n\n" + c.Label + ": " + c.Comment
		profile = m.Profile
	}
	pax := map[string]string{
		"comment":          comment,
		"HANDOFF.format":   storeFormat,
		"HANDOFF.head":     c.ID,
		"HANDOFF.label":    c.Label,
		"HANDOFF.versions": fmt.Sprint(st.Versions),
		"HANDOFF.profile":  profile,
	}
	if err := writePackage(s, out, pax, bundleBin); err != nil {
		die("pack: %v", err)
	}
	fi, _ := os.Stat(out)
	return map[string]any{"wrote": out, "bytes": fi.Size(), "head": c.ID, "label": c.Label,
		"versions": st.Versions, "bundled_binaries": bundleBin != ""}
}

func cmdPack(dir string, args []string) {
	fs := newFlags("pack --out FILE")
	out := fs.String("out", "", "output .tar.gz")
	bundle := fs.String("bundle-bin", "", "directory of handoff binaries to ship under .handoff/bin/ (self-contained package)")
	allowDirty := fs.Bool("allow-dirty", false, "pack even with uncommitted changes (NOT recommended)")
	_ = parseArgs(fs, args)
	if *out == "" {
		die("pack: --out is required")
	}
	s := mustStore(dir)
	emitJSON(packInto(s, *out, *bundle, *allowDirty))
}

func cmdPackage(dir string, args []string) {
	fs := newFlags("package --data FILE -m \"why\" --out FILE")
	data := fs.String("data", "", "JSON data file")
	msg := fs.String("m", "", "comment (MANDATORY)")
	out := fs.String("out", "", "output .tar.gz")
	profile := fs.String("profile", "", "vocabulary preset")
	skill := fs.String("unpacker-skill", "", "unpacker SKILL directory to bundle")
	bundle := fs.String("bundle-bin", "", "binaries to ship under .handoff/bin/")
	_ = parseArgs(fs, args)
	if *data == "" || *out == "" {
		die("package: --data and --out are required")
	}
	if strings.TrimSpace(*msg) == "" {
		die("package: %v", errNoComment)
	}
	s, err := findStore(dir)
	if err != nil {
		if s, err = initStore(dir); err != nil {
			die("package: %v", err)
		}
	}
	m, err := scaffold(s.root, *data, *profile, *skill)
	if err != nil {
		die("package: %v", err)
	}
	c, err := s.commitTree(*msg, false, time.Now())
	if err != nil {
		die("package: %v", err)
	}
	r := packInto(s, *out, *bundle, false)
	r["profile"], r["read_order"], r["comment"] = m.Profile, m.ReadOrder, c.Comment
	emitJSON(r)
}

func cmdInspect(args []string) {
	fs := newFlags("inspect FILE.tar.gz")
	human := fs.Bool("human", false, "plain-text output")
	pos := parseArgs(fs, args)
	if len(pos) != 1 {
		die("inspect: one package file is required")
	}
	r, err := inspect(pos[0])
	if err != nil {
		die("inspect: %v", err)
	}
	if !*human {
		emitJSON(r)
		return
	}
	fmt.Printf("%s — %d files, %d versions, head %s (%s)\n", r.File, r.Files, len(r.Versions), r.Pax["HANDOFF.label"], short(r.Pax["HANDOFF.head"]))
	if c := r.Pax["comment"]; c != "" {
		fmt.Println("\n" + c + "\n")
	}
	for _, v := range r.Versions {
		fmt.Printf("%-4s %s  %s  +%d ~%d -%d  %s\n", v.Label, short(v.ID), v.Timestamp, len(v.Added), len(v.Modified), len(v.Deleted), firstLine(v.Comment))
	}
	if len(r.Bundled) > 0 {
		fmt.Println("bundled binaries: " + strings.Join(r.Bundled, ", "))
	}
}

func optArg(pos []string, i int) string {
	if i < len(pos) {
		return pos[i]
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
