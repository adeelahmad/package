// scaffold.go — renders the package's standard documents from a data file the agent
// authors. This is the seam between the two pathways: WHAT goes in the package is the
// agent's judgment (data.json); HOW it is laid out, validated, versioned and shipped is
// this binary's — deterministic, templated, never hard-coded.
//
// The format is domain-neutral. A "workstream" is any independent track of work; each
// carries a PLAN DOCUMENT whose filename comes from the profile. Software is one
// optional preset, not an assumption.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// profiles map a preset name to the plan-document filename workstreams must carry.
var profiles = map[string]string{
	"":         "plan.md", // default: fully generic
	"general":  "plan.md",
	"research": "plan.md",
	"software": "stories.md", // agentic-agile compatible
}

const manifestName = "handoff.manifest.json"

type handoffData struct {
	Intake      string            `json:"intake"`
	Settled     []string          `json:"settled"`
	Open        []string          `json:"open"`
	Workstreams []string          `json:"workstreams"`
	Invariants  []string          `json:"invariants"`
	Facts       []string          `json:"facts"`
	Tasks       []string          `json:"tasks"`
	Overview    string            `json:"overview"`
	ReadOrder   []string          `json:"read_order"`
	ExtraDocs   map[string]string `json:"extra_docs"`
	Profile     string            `json:"profile"`
}

type manifest struct {
	Kind         string   `json:"kind"`
	Spec         string   `json:"spec"`
	Profile      string   `json:"profile"`
	PlanDoc      string   `json:"plan_doc"`
	Entry        string   `json:"entry"`
	ReadOrder    []string `json:"read_order"`
	Intake       string   `json:"intake"`
	Settled      []string `json:"settled"`
	Open         []string `json:"open"`
	Tasks        []string `json:"tasks"`
	Workstreams  []string `json:"workstreams"`
	BundledSkill string   `json:"bundled_skill,omitempty"`
	History      string   `json:"history"`
}

func loadTemplates() (*template.Template, error) {
	return template.ParseFS(templatesFS, "templates/*.tmpl")
}

func render(t *template.Template, name string, d handoffData) (string, error) {
	var sb strings.Builder
	if err := t.ExecuteTemplate(&sb, name, d); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return sb.String(), nil
}

// validateData enforces the structural rules the agent can get wrong: the deterministic
// side defending the seam.
func validateData(d handoffData) error {
	if strings.TrimSpace(d.Intake) == "" {
		return errors.New("data: 'intake' is required (one paragraph telling the receiving agent what this is)")
	}
	if len(d.Workstreams) == 0 {
		return errors.New("data: at least one workstream is required")
	}
	if _, ok := profiles[d.Profile]; !ok {
		return fmt.Errorf("data: unknown profile %q (choose general, research, software, or omit)", d.Profile)
	}
	seen := map[string]bool{}
	for _, s := range d.Settled {
		seen[strings.TrimSpace(s)] = true
	}
	for _, o := range d.Open {
		if seen[strings.TrimSpace(o)] {
			return fmt.Errorf("data: %q is listed as both settled and open — pick one", o)
		}
	}
	ws := map[string]bool{}
	for _, w := range d.Workstreams {
		if strings.Contains(w, "..") || filepath.IsAbs(w) {
			return fmt.Errorf("data: workstream %q must be a relative path inside the package", w)
		}
		if ws[w] {
			return fmt.Errorf("data: workstream %q listed twice", w)
		}
		ws[w] = true
	}
	return nil
}

// scaffold renders the standard docs into root, bundles the unpacker skill if given,
// validates every workstream carries its plan document, and writes the manifest.
func scaffold(root, dataPath, profile, unpackerSkill string) (*manifest, error) {
	b, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}
	var d handoffData
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("parse data: %w", err)
	}
	if profile != "" {
		d.Profile = profile
	}
	if err := validateData(d); err != nil {
		return nil, err
	}
	planDoc := profiles[d.Profile]

	t, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	docs := map[string]string{}
	for out, tmpl := range map[string]string{
		"AGENTS.md":                 "AGENTS.md.tmpl",
		"docs/ESTABLISHED-FACTS.md": "ESTABLISHED-FACTS.md.tmpl",
		"docs/overview.md":          "overview.md.tmpl",
	} {
		s, err := render(t, tmpl, d)
		if err != nil {
			return nil, err
		}
		docs[out] = s
	}
	for rel, content := range d.ExtraDocs {
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("extra_docs: %q must be a relative path", rel)
		}
		docs["docs/"+rel] = content
	}
	for rel, content := range docs {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return nil, err
		}
	}

	m := &manifest{Kind: "agent-handoff-package", Spec: "2.0", Profile: d.Profile, PlanDoc: planDoc,
		Entry: "AGENTS.md", Intake: d.Intake, Settled: d.Settled, Open: d.Open, Tasks: d.Tasks,
		Workstreams: d.Workstreams,
		History:     "all prior versions are sealed in " + storeDir + "/ — read them ONLY through the handoff binary (history/show/diff/extract)"}
	for _, f := range []*[]string{&m.Settled, &m.Open, &m.Tasks} {
		if *f == nil {
			*f = []string{}
		}
	}

	if unpackerSkill != "" {
		if err := bundleSkill(root, unpackerSkill); err != nil {
			return nil, err
		}
		m.BundledSkill = ".skills/unpacker/SKILL.md"
	}

	// every workstream must carry the profile's plan document
	m.ReadOrder = d.ReadOrder
	if len(m.ReadOrder) == 0 {
		m.ReadOrder = []string{"AGENTS.md", "docs/ESTABLISHED-FACTS.md", "docs/overview.md"}
	}
	for _, ws := range d.Workstreams {
		plans, err := findPlanDocs(root, ws, planDoc)
		if err != nil {
			return nil, err
		}
		if len(plans) == 0 {
			return nil, fmt.Errorf("workstream %q has no %s (profile %q)", ws, planDoc, d.Profile)
		}
		if len(d.ReadOrder) == 0 {
			m.ReadOrder = append(m.ReadOrder, plans...)
		}
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(root, manifestName), append(mb, '\n'), 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

func findPlanDocs(root, ws, planDoc string) ([]string, error) {
	var out []string
	wsRoot := filepath.Join(root, filepath.FromSlash(ws))
	if fi, err := os.Stat(wsRoot); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("workstream %q is not a directory under %s", ws, root)
	}
	filepath.WalkDir(wsRoot, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == planDoc {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out, nil
}

func bundleSkill(root, skillDir string) error {
	fi, err := os.Stat(skillDir)
	if err != nil || !fi.IsDir() {
		return fmt.Errorf("--unpacker-skill %q is not a directory", skillDir)
	}
	dst := filepath.Join(root, ".skills", "unpacker")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(skillDir)
	if err != nil {
		return err
	}
	for _, en := range ents {
		if en.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(skillDir, en.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, en.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// readManifest returns the working tree's manifest if present (nil if absent).
func readManifest(root string) *manifest {
	b, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return nil
	}
	var m manifest
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}
