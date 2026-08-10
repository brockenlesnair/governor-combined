package rigour

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*.yaml
var builtinTemplatesFS embed.FS

// FixTemplate is a reusable fix packet template for common vulnerability patterns.
type FixTemplate struct {
	Name         string       `yaml:"name" json:"name"`
	Description  string       `yaml:"description" json:"description"`
	Severity     string       `yaml:"severity" json:"severity"`
	Category     string       `yaml:"category" json:"category"`
	AstGrep      string       `yaml:"ast_grep_pattern" json:"ast_grep_pattern"`
	Instructions []string     `yaml:"instructions" json:"instructions"`
	Verification []string     `yaml:"verification" json:"verification"`
	Constraints  tmplContraints `yaml:"constraints" json:"constraints"`
}

// tmplContraints mirrors Constraints but uses YAML-friendly types.
type tmplContraints struct {
	DoNotTouch []string `yaml:"do_not_touch" json:"do_not_touch"`
	MaxFiles   int      `yaml:"max_files" json:"max_files"`
	Paradigm   string   `yaml:"paradigm" json:"paradigm"`
}

// ToFixPacket converts a FixTemplate into a FixPacket for use with the existing pipeline.
func (ft *FixTemplate) ToFixPacket(targetPath string) *FixPacket {
	fp := &FixPacket{
		GateName:     ft.Name,
		Severity:     ParseSeverity(ft.Severity),
		Instructions: ft.Instructions,
		Verification: ft.Verification,
		Constraints: Constraints{
			DoNotTouch: ft.Constraints.DoNotTouch,
			MaxFiles:   ft.Constraints.MaxFiles,
			Paradigm:   ft.Constraints.Paradigm,
		},
	}

	if targetPath != "" {
		fp.Files = []FileTarget{{Path: targetPath}}
	}

	return fp
}

// TemplateLoader discovers and loads FixTemplate definitions from YAML files.
type TemplateLoader struct {
	searchPaths []string
}

// NewTemplateLoader creates a loader that checks the embedded templates first,
// then optional additional directories.
func NewTemplateLoader(extraPaths ...string) *TemplateLoader {
	paths := []string{}
	paths = append(paths, extraPaths...)
	return &TemplateLoader{searchPaths: paths}
}

// List returns the names of all available built-in templates.
func (tl *TemplateLoader) List() ([]string, error) {
	entries, err := builtinTemplatesFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read embedded templates: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		names = append(names, name)
	}

	// Also scan extra search paths
	for _, sp := range tl.searchPaths {
		matches, err := filepath.Glob(filepath.Join(sp, "*.yaml"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			base := filepath.Base(m)
			name := strings.TrimSuffix(base, ".yaml")
			// Don't duplicate embedded names
			dup := false
			for _, n := range names {
				if n == name {
					dup = true
					break
				}
			}
			if !dup {
				names = append(names, name)
			}
		}
	}

	return names, nil
}

// Load retrieves a FixTemplate by name. It checks embedded templates first,
// then searches extra paths.
func (tl *TemplateLoader) Load(name string) (*FixTemplate, error) {
	// Try embedded first
	embeddedPath := filepath.Join("templates", name+".yaml")
	data, err := builtinTemplatesFS.ReadFile(embeddedPath)
	if err == nil {
		return parseTemplate(data)
	}

	// Try extra search paths
	for _, sp := range tl.searchPaths {
		filePath := filepath.Join(sp, name+".yaml")
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		return parseTemplate(data)
	}

	return nil, fmt.Errorf("template %q not found", name)
}

// LoadAll returns all available templates.
func (tl *TemplateLoader) LoadAll() ([]*FixTemplate, error) {
	names, err := tl.List()
	if err != nil {
		return nil, err
	}

	var templates []*FixTemplate
	for _, name := range names {
		tmpl, err := tl.Load(name)
		if err != nil {
			return nil, fmt.Errorf("load template %q: %w", name, err)
		}
		templates = append(templates, tmpl)
	}

	return templates, nil
}

// LoadFromDir loads all YAML templates from an arbitrary directory.
func LoadFromDir(dir string) ([]*FixTemplate, error) {
	var templates []*FixTemplate

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		tmpl, err := parseTemplate(data)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		templates = append(templates, tmpl)
		return nil
	})

	return templates, err
}

// TemplateToJSON serializes a template to JSON for display.
func TemplateToJSON(tmpl *FixTemplate) (string, error) {
	data, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parseTemplate(data []byte) (*FixTemplate, error) {
	var tmpl FixTemplate
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	if tmpl.Name == "" {
		return nil, fmt.Errorf("template missing required 'name' field")
	}
	return &tmpl, nil
}
