package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDiscoverFindAndLintEdges(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alpha, "SKILL.md"), []byte("---\nname: Alpha\ndescription: First skill\nmetadata:\n  owner: test\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(alpha, "skill.yaml"), []byte("description: Manifest description\ntriggers: [how do i get there]\ncommands:\n  - name: journey\n    exec: [echo, ok]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beta := filepath.Join(root, "beta")
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beta, "SKILL.md"), []byte("No frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".hidden", "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden", "ignored", "SKILL.md"), []byte("---\nname: hidden\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := Discover([]string{"", filepath.Join(root, "missing"), root})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("skills=%+v", skills)
	}
	alphaSkill, ok := Find(skills, "alpha")
	if !ok {
		t.Fatalf("case-insensitive find failed: %+v", skills)
	}
	if alphaSkill.Description != "First skill" || alphaSkill.Manifest == nil || alphaSkill.Manifest.Name != "Alpha" {
		t.Fatalf("unexpected alpha skill: %+v", alphaSkill)
	}
	if _, ok := Find(skills, "missing"); ok {
		t.Fatal("unexpected missing skill")
	}

	issues := Lint([]Skill{
		alphaSkill,
		{Name: "alpha", Path: "duplicate", Description: "dup"},
		{Name: "bad", Path: "bad", Manifest: &Manifest{Name: "other", Commands: []Command{{}, {Name: "empty"}}}},
		{Name: "nodesc", Path: "nodesc"},
		{Name: "other", Path: "other", Description: "Other", Manifest: &Manifest{Triggers: []string{" how   do I get there "}}},
	})
	want := []string{"duplicate skill name", "skill.yaml name does not match", "missing description", "manifest command is missing name", "has no exec argv", "overlaps"}
	for _, snippet := range want {
		var found bool
		for _, issue := range issues {
			if strings.Contains(issue.Message, snippet) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing lint issue %q in %+v", snippet, issues)
		}
	}
}

func TestLoadAndFrontmatterErrors(t *testing.T) {
	root := t.TempDir()
	badFrontmatter := filepath.Join(root, "bad-frontmatter")
	if err := os.MkdirAll(badFrontmatter, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badFrontmatter, "SKILL.md"), []byte("---\nname: [unterminated\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(badFrontmatter); err == nil {
		t.Fatal("expected frontmatter parse error")
	}

	badManifest := filepath.Join(root, "bad-manifest")
	if err := os.MkdirAll(badManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badManifest, "SKILL.md"), []byte("---\nname: bad-manifest\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badManifest, "skill.yaml"), []byte("name: [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(badManifest); err == nil {
		t.Fatal("expected manifest parse error")
	}

	if _, err := ParseFrontmatter([]byte("---\nname: missing close")); err == nil {
		t.Fatal("expected unterminated frontmatter")
	}
}

func TestDoctorEnvAndManifestOptionalPaths(t *testing.T) {
	t.Setenv("LONDONJOURNEYCLI_TEST_PRESENT", "1")
	missingSkill := Skill{Name: "missing", SkillMD: filepath.Join(t.TempDir(), "SKILL.md")}
	checks := Doctor(missingSkill)
	if len(checks) != 2 || checks[0].OK || !checks[1].OK {
		t.Fatalf("unexpected optional-manifest checks: %+v", checks)
	}

	s := Skill{Name: "env", SkillMD: filepath.Join(t.TempDir(), "SKILL.md"), Manifest: &Manifest{Requires: Requires{Env: []string{"LONDONJOURNEYCLI_TEST_PRESENT", "LONDONJOURNEYCLI_TEST_ABSENT"}}}}
	if err := os.WriteFile(s.SkillMD, []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(s)
	var sawPresent, sawAbsent bool
	for _, check := range checks {
		switch check.Name {
		case "env:LONDONJOURNEYCLI_TEST_PRESENT":
			sawPresent = check.OK
		case "env:LONDONJOURNEYCLI_TEST_ABSENT":
			sawAbsent = !check.OK && strings.Contains(check.Message, "missing")
		}
	}
	if !sawPresent || !sawAbsent {
		t.Fatalf("env checks missing: %+v", checks)
	}
}
