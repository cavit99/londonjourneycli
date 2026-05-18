package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	raw := []byte("---\nname: tfl-journey\ndescription: London journeys\n---\n# Body\n")
	fm, err := ParseFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Name != "tfl-journey" || fm.Description != "London journeys" {
		t.Fatalf("unexpected frontmatter: %+v", fm)
	}

	withDashes := []byte("---\nname: demo\ndescription: \"A --- B\"\n---\n# Body\n")
	fm, err = ParseFrontmatter(withDashes)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Name != "demo" || fm.Description != "A --- B" {
		t.Fatalf("unexpected dashed frontmatter: %+v", fm)
	}

	blockScalar := []byte("---\nname: block\ndescription: |\n  before\n  ---\n  after\n---\n# Body\n")
	fm, err = ParseFrontmatter(blockScalar)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Name != "block" || fm.Description != "before\n---\nafter" {
		t.Fatalf("unexpected block scalar frontmatter: %+v", fm)
	}
}

func TestDiscoverAndLint(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo skill\n---\n# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: demo\ntriggers:\n  - demo thing\ncommands:\n  - name: echo\n    exec: [echo, ok]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills, err := Discover([]string{filepath.Join(dir, "skills")})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "demo" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
	if issues := Lint(skills); len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
}
