// archive.go — the distributable form: <name>.tar.gz containing the working tree (the
// ONE visible version) plus the sealed .handoff/ store (every prior version, deduped).
//
// Metadata rides in the tar's PAX global header so it can be read BEFORE extraction:
//
//	comment            the intake note (and the head version's comment)
//	HANDOFF.format     store format marker
//	HANDOFF.head       id of the head version
//	HANDOFF.label      its label (vN)
//	HANDOFF.versions   how many versions the store holds
//	HANDOFF.profile    vocabulary profile, if scaffolded with one
package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// never shipped inside a package (the store IS shipped; VCS metadata is not)
var packExcludes = map[string]bool{".git": true, ".DS_Store": true}

func writePackage(s *store, out string, pax map[string]string, bundleBin string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	base := filepath.Base(s.root)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: pax, Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	err = filepath.Walk(s.root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.root, p)
		if rel == "." {
			return nil
		}
		if packExcludes[info.Name()] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(filepath.Join(base, rel))
		h.Format = tar.FormatPAX
		if info.IsDir() {
			h.Name += "/"
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
	if err != nil {
		return err
	}
	if bundleBin == "" {
		return nil
	}
	// self-contained option: ship the reader binaries under .handoff/bin/ (tar only —
	// never written into the on-disk store, never part of any version's tree)
	ents, err := os.ReadDir(bundleBin)
	if err != nil {
		return fmt.Errorf("--bundle-bin: %w", err)
	}
	for _, en := range ents {
		if en.IsDir() {
			continue
		}
		p := filepath.Join(bundleBin, en.Name())
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		h, _ := tar.FileInfoHeader(info, "")
		h.Name = filepath.ToSlash(filepath.Join(base, storeDir, "bin", en.Name()))
		h.Mode = 0o755
		h.Format = tar.FormatPAX
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, in); err != nil {
			in.Close()
			return err
		}
		in.Close()
	}
	return nil
}

func openTar(path string) (*tar.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	gz, gerr := gzip.NewReader(f)
	if gerr == nil {
		return tar.NewReader(gz), func() { gz.Close(); f.Close() }, nil
	}
	f.Seek(0, 0)
	return tar.NewReader(f), func() { f.Close() }, nil
}

type inspectReport struct {
	File     string            `json:"file"`
	Pax      map[string]string `json:"pax"`
	Manifest json.RawMessage   `json:"manifest,omitempty"`
	Versions []historyEntry    `json:"versions"`
	Bundled  []string          `json:"bundled_binaries"`
	Files    int               `json:"files"`
}

// inspect reads everything worth knowing about a package WITHOUT extracting it:
// the PAX metadata, the manifest, the full version history, and any bundled binaries.
func inspect(path string) (*inspectReport, error) {
	tr, closeFn, err := openTar(path)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	r := &inspectReport{File: path, Pax: map[string]string{}, Versions: []historyEntry{}, Bundled: []string{}}
	commits := map[string]commit{}
	var order []logLine
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			for k, v := range h.PAXRecords {
				r.Pax[k] = v
			}
			continue
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		r.Files++
		parts := strings.Split(h.Name, "/")
		if len(parts) < 2 {
			continue
		}
		inner := strings.Join(parts[1:], "/")
		switch {
		case inner == "handoff.manifest.json":
			b, _ := io.ReadAll(tr)
			if json.Valid(b) {
				r.Manifest = b
			}
		case inner == storeDir+"/log":
			b, _ := io.ReadAll(tr)
			for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				if f := strings.Split(ln, "\t"); len(f) == 3 {
					order = append(order, logLine{ID: f[0], Label: f[1], Timestamp: f[2]})
				}
			}
		case strings.HasPrefix(inner, storeDir+"/commits/"):
			b, _ := io.ReadAll(tr)
			var c commit
			if json.Unmarshal(b, &c) == nil {
				commits[c.ID] = c
			}
		case strings.HasPrefix(inner, storeDir+"/bin/"):
			r.Bundled = append(r.Bundled, strings.TrimPrefix(inner, storeDir+"/bin/"))
		}
	}
	for i := len(order) - 1; i >= 0; i-- {
		c, ok := commits[order[i].ID]
		if !ok {
			continue
		}
		parentTree := map[string]entry{}
		if p, ok := commits[c.Parent]; ok {
			parentTree = p.Tree
		}
		d := diffTrees(parentTree, c.Tree, ".")
		r.Versions = append(r.Versions, historyEntry{Label: c.Label, ID: c.ID, Timestamp: c.Timestamp,
			Comment: c.Comment, Added: d.Added, Modified: d.Modified, Deleted: d.Deleted})
	}
	sort.Strings(r.Bundled)
	return r, nil
}
