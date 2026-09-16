// store.go — the deterministic history engine.
//
// A working tree has ONE visible version: its current files. All prior versions live
// sealed inside a hidden git-style store, .handoff/, and are reached only through this
// binary. Nothing here involves judgment: same inputs, same bytes, every time.
//
//	.handoff/
//	  FORMAT            format marker
//	  HEAD              id of the latest commit ("" until the first commit)
//	  log               append-only, oldest first: "<id>\t<label>\t<timestamp>"
//	  objects/aa/bbbb…  file contents addressed by sha256 — each unique file stored ONCE
//	  commits/<id>.json commit object: label, parent, comment, timestamp, tree
//
// A commit's tree maps every path in the working tree to {hash,size,mode}. Unchanged
// files across versions cost zero bytes: dedup is by content hash, per file.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	storeDir    = ".handoff"
	storeFormat = "agent-handoff-store 1"
)

// never part of a version's tree (the store itself, VCS metadata, OS litter)
var treeExcludes = map[string]bool{storeDir: true, ".git": true, ".DS_Store": true}

type entry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Mode uint32 `json:"mode"`
}

type commit struct {
	ID        string           `json:"id"`
	Label     string           `json:"label"`
	Parent    string           `json:"parent,omitempty"`
	Comment   string           `json:"comment"`
	Timestamp string           `json:"timestamp"`
	Tree      map[string]entry `json:"tree"`
}

type logLine struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Timestamp string `json:"timestamp"`
}

type store struct {
	root string // working tree root
	dir  string // root/.handoff
}

// ---------- locating / creating ----------

// findStore walks upward from start (like git) until it finds a .handoff/ directory.
func findStore(start string) (*store, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for dir := abs; ; dir = filepath.Dir(dir) {
		if fi, e := os.Stat(filepath.Join(dir, storeDir, "FORMAT")); e == nil && !fi.IsDir() {
			return &store{root: dir, dir: filepath.Join(dir, storeDir)}, nil
		}
		if filepath.Dir(dir) == dir {
			return nil, fmt.Errorf("no %s store found at or above %s (run: handoff init)", storeDir, abs)
		}
	}
}

func initStore(root string) (*store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	s := &store{root: abs, dir: filepath.Join(abs, storeDir)}
	if _, e := os.Stat(filepath.Join(s.dir, "FORMAT")); e == nil {
		return s, nil // already initialised — idempotent
	}
	for _, d := range []string{"objects", "commits"} {
		if err := os.MkdirAll(filepath.Join(s.dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	if err := atomicWrite(filepath.Join(s.dir, "HEAD"), nil); err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(s.dir, "log"), nil); err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(s.dir, "FORMAT"), []byte(storeFormat+"\n")); err != nil {
		return nil, err
	}
	return s, nil
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- objects ----------

func (s *store) objectPath(hash string) string {
	return filepath.Join(s.dir, "objects", hash[:2], hash[2:])
}

// hashFile hashes a file; with persist it also stores the bytes as an object (once).
func (s *store) hashFile(path string, persist bool) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	if !persist {
		n, err := io.Copy(h, f)
		return hex.EncodeToString(h.Sum(nil)), n, err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "objects"), "incoming-*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	n, err := io.Copy(io.MultiWriter(h, tmp), f)
	tmp.Close()
	if err != nil {
		os.Remove(tmpName)
		return "", 0, err
	}
	hash := hex.EncodeToString(h.Sum(nil))
	dst := s.objectPath(hash)
	if _, e := os.Stat(dst); e == nil {
		os.Remove(tmpName) // already stored — dedup
		return hash, n, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		os.Remove(tmpName)
		return "", 0, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return "", 0, err
	}
	os.Chmod(dst, 0o444)
	return hash, n, nil
}

// readObject returns an object's bytes, verifying its hash (integrity on every read).
func (s *store) readObject(hash string) ([]byte, error) {
	b, err := os.ReadFile(s.objectPath(hash))
	if err != nil {
		return nil, fmt.Errorf("object %s missing from store: %w", hash[:12], err)
	}
	if got := sha256hex(b); got != hash {
		return nil, fmt.Errorf("object %s is corrupt (content hashes to %s)", hash[:12], got[:12])
	}
	return b, nil
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *store) countObjects() int {
	n := 0
	filepath.WalkDir(filepath.Join(s.dir, "objects"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasPrefix(d.Name(), "incoming-") {
			n++
		}
		return nil
	})
	return n
}

// ---------- trees ----------

// scanTree walks the working tree and hashes every regular file. With persist, the
// contents are also written to the object store.
func (s *store) scanTree(persist bool) (map[string]entry, error) {
	tree := map[string]entry{}
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if treeExcludes[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if treeExcludes[d.Name()] || !d.Type().IsRegular() {
			return nil // symlinks, sockets, litter: not part of a version
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hash, size, err := s.hashFile(p, persist)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = entry{Hash: hash, Size: size, Mode: uint32(info.Mode().Perm())}
		return nil
	})
	return tree, err
}

type treeDiff struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

func (d treeDiff) empty() bool { return len(d.Added)+len(d.Modified)+len(d.Deleted) == 0 }

// diffTrees compares two trees, restricted to paths matching query ("." = everything).
func diffTrees(from, to map[string]entry, query string) treeDiff {
	d := treeDiff{Added: []string{}, Modified: []string{}, Deleted: []string{}}
	for p, e := range to {
		if !pathMatches(p, query) {
			continue
		}
		if old, ok := from[p]; !ok {
			d.Added = append(d.Added, p)
		} else if old.Hash != e.Hash {
			d.Modified = append(d.Modified, p)
		}
	}
	for p := range from {
		if _, ok := to[p]; !ok && pathMatches(p, query) {
			d.Deleted = append(d.Deleted, p)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Modified)
	sort.Strings(d.Deleted)
	return d
}

// pathMatches: query "." matches everything; otherwise the path itself or anything under it.
func pathMatches(path, query string) bool {
	return query == "." || path == query || strings.HasPrefix(path, query+"/")
}

// normPath turns a user-supplied path (relative, "./x", absolute inside root, trailing /)
// into the tree's canonical slash form. "" and "." mean the whole tree.
func (s *store) normPath(p string) (string, error) {
	if p == "" || p == "." {
		return ".", nil
	}
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(s.root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("%s is outside the working tree %s", p, s.root)
		}
		p = rel
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." {
		return ".", nil
	}
	if strings.HasPrefix(p, "../") || p == ".." {
		return "", fmt.Errorf("%s escapes the working tree", p)
	}
	return strings.TrimPrefix(p, "./"), nil
}

// ---------- commits ----------

func (s *store) head() string {
	b, _ := os.ReadFile(filepath.Join(s.dir, "HEAD"))
	return strings.TrimSpace(string(b))
}

func (s *store) log() ([]logLine, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "log"))
	if err != nil {
		return nil, err
	}
	var lines []logLine
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if ln == "" {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("corrupt log line: %q", ln)
		}
		lines = append(lines, logLine{ID: f[0], Label: f[1], Timestamp: f[2]})
	}
	return lines, nil
}

func (s *store) loadCommit(id string) (*commit, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, "commits", id+".json"))
	if err != nil {
		return nil, fmt.Errorf("commit %s not found: %w", short(id), err)
	}
	var c commit
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("commit %s unreadable: %w", short(id), err)
	}
	return &c, nil
}

// commitID is the sha256 of the canonical JSON of everything but the id itself.
func commitID(c commit) string {
	body := struct {
		Label     string           `json:"label"`
		Parent    string           `json:"parent"`
		Comment   string           `json:"comment"`
		Timestamp string           `json:"timestamp"`
		Tree      map[string]entry `json:"tree"`
	}{c.Label, c.Parent, c.Comment, c.Timestamp, c.Tree}
	b, _ := json.Marshal(body) // map keys are sorted by encoding/json → canonical
	return sha256hex(b)
}

var errNoComment = errors.New("a comment is mandatory — every version must say why it exists: handoff commit -m \"...\"")

// commitTree snapshots the working tree as a new version. The comment is mandatory.
// Empty versions (nothing changed) are refused unless allowEmpty.
func (s *store) commitTree(comment string, allowEmpty bool, now time.Time) (*commit, error) {
	if strings.TrimSpace(comment) == "" {
		return nil, errNoComment
	}
	tree, err := s.scanTree(true)
	if err != nil {
		return nil, err
	}
	lines, err := s.log()
	if err != nil {
		return nil, err
	}
	parentID := s.head()
	if parentID != "" {
		parent, err := s.loadCommit(parentID)
		if err != nil {
			return nil, err
		}
		if !allowEmpty && diffTrees(parent.Tree, tree, ".").empty() {
			return nil, fmt.Errorf("nothing changed since %s — refusing to record an empty version (use --allow-empty to record one anyway)", parent.Label)
		}
	} else if len(tree) == 0 && !allowEmpty {
		return nil, errors.New("the working tree is empty — nothing to version")
	}
	c := commit{
		Label:     fmt.Sprintf("v%d", len(lines)+1),
		Parent:    parentID,
		Comment:   strings.TrimSpace(comment),
		Timestamp: now.UTC().Format(time.RFC3339),
		Tree:      tree,
	}
	c.ID = commitID(c)
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := atomicWrite(filepath.Join(s.dir, "commits", c.ID+".json"), b); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(filepath.Join(s.dir, "log"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(lf, "%s\t%s\t%s\n", c.ID, c.Label, c.Timestamp); err != nil {
		lf.Close()
		return nil, err
	}
	lf.Close()
	if err := atomicWrite(filepath.Join(s.dir, "HEAD"), []byte(c.ID+"\n")); err != nil {
		return nil, err
	}
	return &c, nil
}

// ---------- refs ----------

// resolve turns a human/agent ref into a commit:
//
//	latest | HEAD | @      the newest version
//	vN                     a version label
//	<id> or ≥4-char prefix a commit id
//	any of the above ~N    N versions back from it (HEAD~1 = the previous version)
func (s *store) resolve(ref string) (*commit, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "latest"
	}
	base, back := ref, 0
	if i := strings.Index(ref, "~"); i >= 0 {
		n, err := strconv.Atoi(ref[i+1:])
		if err != nil || n < 0 {
			return nil, fmt.Errorf("bad ref %q (use e.g. latest, v2, HEAD~1, or a commit id)", ref)
		}
		base, back = ref[:i], n
	}
	lines, err := s.log()
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, errors.New("no versions yet — nothing has been committed")
	}
	id := ""
	switch strings.ToLower(base) {
	case "latest", "head", "@":
		id = lines[len(lines)-1].ID
	default:
		for _, l := range lines {
			if l.Label == base {
				id = l.ID
			}
		}
		if id == "" && len(base) >= 4 {
			var hits []string
			for _, l := range lines {
				if strings.HasPrefix(l.ID, base) {
					hits = append(hits, l.ID)
				}
			}
			switch len(hits) {
			case 1:
				id = hits[0]
			case 0:
			default:
				return nil, fmt.Errorf("ref %q is ambiguous (%d commits match)", base, len(hits))
			}
		}
		if id == "" {
			return nil, fmt.Errorf("unknown version %q (have %s..%s)", base, lines[0].Label, lines[len(lines)-1].Label)
		}
	}
	c, err := s.loadCommit(id)
	if err != nil {
		return nil, err
	}
	for i := 0; i < back; i++ {
		if c.Parent == "" {
			return nil, fmt.Errorf("%s has no version %d back (%s is the first)", ref, back, c.Label)
		}
		if c, err = s.loadCommit(c.Parent); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ---------- history ----------

type historyEntry struct {
	Label     string   `json:"label"`
	ID        string   `json:"id"`
	Timestamp string   `json:"timestamp"`
	Comment   string   `json:"comment"`
	Change    string   `json:"change,omitempty"` // for a single file: added|modified|deleted
	Added     []string `json:"added"`
	Modified  []string `json:"modified"`
	Deleted   []string `json:"deleted"`
}

// history lists versions newest-first. With a path (file or directory) only versions
// that changed something matching it are listed, each annotated with what changed.
func (s *store) history(query string) ([]historyEntry, error) {
	lines, err := s.log()
	if err != nil {
		return nil, err
	}
	out := []historyEntry{}
	for i := len(lines) - 1; i >= 0; i-- {
		c, err := s.loadCommit(lines[i].ID)
		if err != nil {
			return nil, err
		}
		parentTree := map[string]entry{}
		if c.Parent != "" {
			p, err := s.loadCommit(c.Parent)
			if err != nil {
				return nil, err
			}
			parentTree = p.Tree
		}
		d := diffTrees(parentTree, c.Tree, query)
		if query != "." && d.empty() {
			continue
		}
		h := historyEntry{Label: c.Label, ID: c.ID, Timestamp: c.Timestamp, Comment: c.Comment,
			Added: d.Added, Modified: d.Modified, Deleted: d.Deleted}
		if query != "." {
			switch {
			case len(d.Added) == 1 && d.Added[0] == query:
				h.Change = "added"
			case len(d.Modified) == 1 && d.Modified[0] == query:
				h.Change = "modified"
			case len(d.Deleted) == 1 && d.Deleted[0] == query:
				h.Change = "deleted"
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// ---------- status ----------

type statusReport struct {
	Root     string   `json:"root"`
	Head     string   `json:"head,omitempty"`
	Label    string   `json:"label,omitempty"`
	Versions int      `json:"versions"`
	Clean    bool     `json:"clean"`
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

func (s *store) status() (statusReport, error) {
	work, err := s.scanTree(false)
	if err != nil {
		return statusReport{}, err
	}
	lines, err := s.log()
	if err != nil {
		return statusReport{}, err
	}
	r := statusReport{Root: s.root, Versions: len(lines)}
	headTree := map[string]entry{}
	if id := s.head(); id != "" {
		c, err := s.loadCommit(id)
		if err != nil {
			return r, err
		}
		headTree, r.Head, r.Label = c.Tree, c.ID, c.Label
	}
	d := diffTrees(headTree, work, ".")
	r.Added, r.Modified, r.Deleted, r.Clean = d.Added, d.Modified, d.Deleted, d.empty()
	return r, nil
}

// ---------- verify ----------

type verifyReport struct {
	OK       bool     `json:"ok"`
	Versions int      `json:"versions"`
	Objects  int      `json:"objects"`
	Problems []string `json:"problems"`
}

// verify re-hashes every object and checks every commit's structure and references.
func (s *store) verify() verifyReport {
	r := verifyReport{Problems: []string{}}
	objRoot := filepath.Join(s.dir, "objects")
	filepath.WalkDir(objRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), "incoming-") {
			return nil
		}
		r.Objects++
		want := filepath.Base(filepath.Dir(p)) + d.Name()
		b, err := os.ReadFile(p)
		if err != nil {
			r.Problems = append(r.Problems, "unreadable object "+want)
			return nil
		}
		if got := sha256hex(b); got != want {
			r.Problems = append(r.Problems, fmt.Sprintf("corrupt object %s (hashes to %s)", short(want), short(got)))
		}
		return nil
	})
	lines, err := s.log()
	if err != nil {
		r.Problems = append(r.Problems, "log: "+err.Error())
		return r
	}
	seen := map[string]bool{}
	for i, l := range lines {
		r.Versions++
		c, err := s.loadCommit(l.ID)
		if err != nil {
			r.Problems = append(r.Problems, err.Error())
			continue
		}
		if commitID(*c) != c.ID {
			r.Problems = append(r.Problems, "commit "+short(c.ID)+" does not match its own content")
		}
		if want := fmt.Sprintf("v%d", i+1); c.Label != want || l.Label != want {
			r.Problems = append(r.Problems, fmt.Sprintf("commit %s labelled %s, expected %s", short(c.ID), c.Label, want))
		}
		if c.Parent != "" && !seen[c.Parent] {
			r.Problems = append(r.Problems, "commit "+short(c.ID)+" has a parent that is not earlier in the log")
		}
		if c.Parent == "" && i != 0 {
			r.Problems = append(r.Problems, "commit "+short(c.ID)+" has no parent but is not the first version")
		}
		for p, e := range c.Tree {
			if _, err := os.Stat(s.objectPath(e.Hash)); err != nil {
				r.Problems = append(r.Problems, fmt.Sprintf("%s: %s references missing object %s", c.Label, p, short(e.Hash)))
			}
		}
		seen[c.ID] = true
	}
	if n := len(lines); n > 0 && s.head() != lines[n-1].ID {
		r.Problems = append(r.Problems, "HEAD does not point at the last logged version")
	}
	r.OK = len(r.Problems) == 0
	return r
}

// ---------- extraction ----------

// extract writes a file or directory (or "." for everything) as it was at commit c
// into dest — standalone, outside the package.
func (s *store) extract(c *commit, query, dest string) ([]string, error) {
	if e, ok := c.Tree[query]; ok { // a single file
		target := dest
		if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
			target = filepath.Join(dest, filepath.Base(query))
		}
		return []string{target}, s.writeObject(e, target)
	}
	var written []string
	for p, e := range c.Tree {
		if !pathMatches(p, query) {
			continue
		}
		rel := p
		if query != "." {
			rel = strings.TrimPrefix(p, query+"/")
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := s.writeObject(e, target); err != nil {
			return written, err
		}
		written = append(written, target)
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("%s does not exist at %s", query, c.Label)
	}
	sort.Strings(written)
	return written, nil
}

func (s *store) writeObject(e entry, target string) error {
	b, err := s.readObject(e.Hash)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(e.Mode)
	if mode == 0 {
		mode = 0o644
	}
	return os.WriteFile(target, b, mode)
}
