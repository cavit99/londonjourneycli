package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cavit99/londonjourneycli/internal/exitcode"
)

func TestListJSON(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "--json", "list"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "\"name\": \"demo\"") {
		t.Fatalf("missing demo in %s", out.String())
	}
}

func TestLintDetectsDuplicateTrigger(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Demo\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: "+name+"\ntriggers: [same trigger]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "lint"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("warnings should not fail lint: code=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "overlaps") {
		t.Fatalf("expected overlap warning, got %s", out.String())
	}
}

func TestDefaultSkillRootsArePortable(t *testing.T) {
	t.Setenv("LONDONJOURNEYCLI_SKILLS_DIR", "")
	roots := skillRoots(globals{})
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	allowedHomeRoots := map[string]bool{
		filepath.Join(home, ".config", "londonjourneycli", "skills"):         true,
		filepath.Join(home, ".local", "share", "londonjourneycli", "skills"): true,
	}
	allowedCwdRoot := filepath.Join(cwd, "skills")
	for _, root := range roots {
		if root == allowedCwdRoot || root == cwd {
			continue
		}
		if strings.HasPrefix(root, home+string(os.PathSeparator)) && !allowedHomeRoots[root] {
			t.Fatalf("default home root should be portable: %v", roots)
		}
	}
}
