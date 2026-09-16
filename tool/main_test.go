package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// a store with two versions: v1 (a.md, ws/plan.md, shared.txt), v2 (a.md changed)
func twoVersions(t *testing.T) *store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "pkg")
	s, err := initStore(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "a.md", "one\ntwo\nthree\n")
	write(t, root, "ws/plan.md", "# plan\n")
	write(t, root, "shared.txt", "same in every version\n")
	if _, err := s.commitTree("v1: first", false, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	write(t, root, "a.md", "one\nTWO\nthree\nfour\n")
	if _, err := s.commitTree("v2: changed a.md", false, time.Unix(2000, 0)); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTemplatesLoad(t *testing.T) {
	if _, err := loadTemplates(); err != nil {
		t.Fatal(err)
	}
}

func TestCommentIsMandatory(t *testing.T) {
	root := t.TempDir()
	s, _ := initStore(root)
	write(t, root, "x.md", "x")
	if _, err := s.commitTree("   ", false, time.Now()); err == nil {
		t.Fatal("commit without a comment must be refused")
	}
	if _, err := s.commitTree("first", false, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyVersionRefused(t *testing.T) {
	s := twoVersions(t)
	if _, err := s.commitTree("nothing changed", false, time.Now()); err == nil {
		t.Fatal("empty version must be refused without --allow-empty")
	}
	if _, err := s.commitTree("recorded anyway", true, time.Now()); err != nil {
		t.Fatalf("--allow-empty should permit it: %v", err)
	}
	lines, _ := s.log()
	if len(lines) != 3 || lines[2].Label != "v3" {
		t.Fatalf("expected v3 after allow-empty, got %+v", lines)
	}
}

func TestDedupStoresEachUniqueFileOnce(t *testing.T) {
	s := twoVersions(t)
	// v1: 3 unique files; v2 changed one → exactly one new object
	if n := s.countObjects(); n != 4 {
		t.Fatalf("expected 4 objects (3 + 1 changed), got %d", n)
	}
	write(t, s.root, "copy-of-shared.txt", "same in every version\n") // identical content
	if _, err := s.commitTree("v3: duplicate content", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := s.countObjects(); n != 4 {
		t.Fatalf("identical content must not add an object: got %d", n)
	}
}

func TestHistoryPerPath(t *testing.T) {
	s := twoVersions(t)
	h, err := s.history("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 || h[0].Label != "v2" || h[0].Change != "modified" || h[1].Label != "v1" || h[1].Change != "added" {
		t.Fatalf("a.md history wrong: %+v", h)
	}
	h, _ = s.history("shared.txt")
	if len(h) != 1 || h[0].Label != "v1" {
		t.Fatalf("unchanged file must appear only where it was added: %+v", h)
	}
	h, _ = s.history("ws") // a directory
	if len(h) != 1 || h[0].Added[0] != "ws/plan.md" {
		t.Fatalf("directory history wrong: %+v", h)
	}
	h, _ = s.history(".")
	if len(h) != 2 {
		t.Fatalf("whole-tree history should list every version: %+v", h)
	}
}

func TestShowAndRefs(t *testing.T) {
	s := twoVersions(t)
	cases := map[string]string{"latest": "one\nTWO\nthree\nfour\n", "HEAD": "one\nTWO\nthree\nfour\n",
		"v1": "one\ntwo\nthree\n", "HEAD~1": "one\ntwo\nthree\n", "v2~1": "one\ntwo\nthree\n"}
	for ref, want := range cases {
		c, err := s.resolve(ref)
		if err != nil {
			t.Fatalf("resolve %s: %v", ref, err)
		}
		b, err := s.readObject(c.Tree["a.md"].Hash)
		if err != nil || string(b) != want {
			t.Fatalf("show a.md at %s: got %q err %v", ref, b, err)
		}
	}
	lines, _ := s.log()
	if c, err := s.resolve(lines[0].ID[:8]); err != nil || c.Label != "v1" {
		t.Fatalf("id-prefix ref failed: %v %v", c, err)
	}
	if _, err := s.resolve("v9"); err == nil {
		t.Fatal("unknown version must error")
	}
	if _, err := s.resolve("v1~1"); err == nil {
		t.Fatal("walking past the first version must error")
	}
}

func TestExtractFileAndDirectory(t *testing.T) {
	s := twoVersions(t)
	c, _ := s.resolve("v1")
	dest := t.TempDir()
	if _, err := s.extract(c, "a.md", filepath.Join(dest, "old-a.md")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "old-a.md")); string(b) != "one\ntwo\nthree\n" {
		t.Fatalf("extracted file wrong: %q", b)
	}
	all := filepath.Join(dest, "all")
	written, err := s.extract(c, ".", all)
	if err != nil || len(written) != 3 {
		t.Fatalf("extract whole tree: %v %v", written, err)
	}
	if b, _ := os.ReadFile(filepath.Join(all, "ws", "plan.md")); string(b) != "# plan\n" {
		t.Fatal("directory extract lost nested file")
	}
	if _, err := s.extract(c, "nope.md", dest); err == nil {
		t.Fatal("extracting a missing path must error")
	}
}

func TestUnifiedDiff(t *testing.T) {
	s := twoVersions(t)
	v1, _ := s.resolve("v1")
	v2, _ := s.resolve("v2")
	a, _ := s.readObject(v1.Tree["a.md"].Hash)
	b, _ := s.readObject(v2.Tree["a.md"].Hash)
	d := unifiedDiff("a.md@v1", "a.md@v2", a, b)
	for _, want := range []string{"--- a.md@v1", "+++ a.md@v2", "-two", "+TWO", "+four", "@@ -1,3 +1,4 @@"} {
		if !strings.Contains(d, want) {
			t.Fatalf("diff missing %q:\n%s", want, d)
		}
	}
	if unifiedDiff("x", "y", a, a) != "" {
		t.Fatal("identical content must produce an empty diff")
	}
	td := diffTrees(v1.Tree, v2.Tree, ".")
	if len(td.Modified) != 1 || td.Modified[0] != "a.md" || len(td.Added)+len(td.Deleted) != 0 {
		t.Fatalf("tree diff wrong: %+v", td)
	}
}

func TestVerifyCatchesCorruption(t *testing.T) {
	s := twoVersions(t)
	if r := s.verify(); !r.OK || r.Versions != 2 || r.Objects != 4 {
		t.Fatalf("fresh store must verify: %+v", r)
	}
	c, _ := s.resolve("v1")
	p := s.objectPath(c.Tree["shared.txt"].Hash)
	os.Chmod(p, 0o644)
	os.WriteFile(p, []byte("tampered"), 0o644)
	r := s.verify()
	if r.OK || len(r.Problems) == 0 || !strings.Contains(r.Problems[0], "corrupt") {
		t.Fatalf("verify must catch a corrupt object: %+v", r)
	}
	if _, err := s.readObject(c.Tree["shared.txt"].Hash); err == nil {
		t.Fatal("reading a corrupt object must fail")
	}
}

func TestStatusAndPackRefusesDirty(t *testing.T) {
	s := twoVersions(t)
	st, _ := s.status()
	if !st.Clean || st.Label != "v2" {
		t.Fatalf("expected clean at v2: %+v", st)
	}
	write(t, s.root, "a.md", "dirty\n")
	st, _ = s.status()
	if st.Clean || len(st.Modified) != 1 {
		t.Fatalf("expected a dirty tree: %+v", st)
	}
	// the store itself must never be part of a version
	tree, _ := s.scanTree(false)
	for p := range tree {
		if strings.HasPrefix(p, storeDir) {
			t.Fatalf("store leaked into the tree: %s", p)
		}
	}
}

func TestPackAndInspect(t *testing.T) {
	s := twoVersions(t)
	out := filepath.Join(t.TempDir(), "pkg.tar.gz")
	pax := map[string]string{"comment": "the intake", "HANDOFF.label": "v2", "HANDOFF.versions": "2"}
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "handoff-linux-amd64"), []byte("fake-binary"), 0o755)
	if err := writePackage(s, out, pax, bin); err != nil {
		t.Fatal(err)
	}
	r, err := inspect(out)
	if err != nil {
		t.Fatal(err)
	}
	if r.Pax["comment"] != "the intake" || r.Pax["HANDOFF.label"] != "v2" {
		t.Fatalf("PAX records did not round-trip: %+v", r.Pax)
	}
	if len(r.Versions) != 2 || r.Versions[0].Label != "v2" || r.Versions[0].Comment != "v2: changed a.md" {
		t.Fatalf("inspect must recover the full history without extraction: %+v", r.Versions)
	}
	if len(r.Bundled) != 1 || r.Bundled[0] != "handoff-linux-amd64" {
		t.Fatalf("bundled binaries not recorded: %+v", r.Bundled)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "bin")); err == nil {
		t.Fatal("--bundle-bin must never write into the on-disk store")
	}
}

func TestScaffoldProfiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "case")
	if _, err := initStore(root); err != nil {
		t.Fatal(err)
	}
	write(t, root, "discovery/plan.md", "# discovery\n")
	data := filepath.Join(t.TempDir(), "d.json")
	os.WriteFile(data, []byte(`{"intake":"Smith v. Jones handoff","workstreams":["discovery"],
	  "settled":["Jurisdiction is NSW"],"open":["Expert witness"],"tasks":["1. discovery"]}`), 0o644)
	m, err := scaffold(root, data, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.PlanDoc != "plan.md" || m.ReadOrder[len(m.ReadOrder)-1] != "discovery/plan.md" {
		t.Fatalf("generic profile wrong: %+v", m)
	}
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if m := regexp.MustCompile(`(?i)\b(repos?|stories|sprints?)\b|lineage/`).Find(agents); m != nil {
		t.Fatalf("generic AGENTS.md leaks software/lineage vocabulary %q", m)
	}
	if !strings.Contains(string(agents), "Jurisdiction is NSW") || !strings.Contains(string(agents), "handoff history") {
		t.Fatal("AGENTS.md missing settled facts or history instructions")
	}
	if _, err := scaffold(root, data, "software", ""); err == nil || !strings.Contains(err.Error(), "stories.md") {
		t.Fatalf("software profile must demand stories.md: %v", err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(filepath.Join(root, manifestName))
	if json.Unmarshal(b, &raw) != nil || raw["kind"] != "agent-handoff-package" {
		t.Fatal("manifest not written")
	}
	// settled/open overlap is a structural error the tool must catch
	os.WriteFile(data, []byte(`{"intake":"x","workstreams":["discovery"],"settled":["A"],"open":["A"]}`), 0o644)
	if _, err := scaffold(root, data, "", ""); err == nil || !strings.Contains(err.Error(), "both settled and open") {
		t.Fatalf("overlapping settled/open must be refused: %v", err)
	}
}

func TestFindStoreWalksUp(t *testing.T) {
	s := twoVersions(t)
	found, err := findStore(filepath.Join(s.root, "ws"))
	if err != nil || found.root != s.root {
		t.Fatalf("findStore from a subdirectory: %v %v", found, err)
	}
	if _, err := findStore(t.TempDir()); err == nil {
		t.Fatal("a directory with no store must error")
	}
}
