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

func TestShowDoctorRunAndTest(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\nrequires:\n  bins: [echo]\ncommands:\n  - name: echo\n    exec: [echo, hello]\ntests:\n  - name: echo-test\n    command: [echo, tested]\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "show", "demo"}, &out, &errb)
	if code != exitcode.OK || !strings.Contains(out.String(), "Demo") {
		t.Fatalf("show failed code=%d out=%s err=%s", code, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"--skills-dir", dir, "doctor", "demo"}, &out, &errb)
	if code != exitcode.OK || !strings.Contains(out.String(), "bin:echo") {
		t.Fatalf("doctor failed code=%d out=%s err=%s", code, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"--skills-dir", dir, "run", "demo", "echo"}, &out, &errb)
	if code != exitcode.OK || strings.TrimSpace(out.String()) != "hello" {
		t.Fatalf("run failed code=%d out=%q err=%s", code, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"--skills-dir", dir, "test", "demo"}, &out, &errb)
	if code != exitcode.OK || strings.TrimSpace(out.String()) != "tested" {
		t.Fatalf("test failed code=%d out=%q err=%s", code, out.String(), errb.String())
	}
}

func TestRunPassesFlagsAfterSubcommand(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\ncommands:\n  - name: echo\n    exec: [echo]\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "run", "demo", "echo", "--json"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("run failed code=%d out=%q err=%s", code, out.String(), errb.String())
	}
	if strings.TrimSpace(out.String()) != "--json" {
		t.Fatalf("expected passthrough flag, got %q", out.String())
	}
}

func TestJSONTestReturnsStructuredResults(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\ntests:\n  - name: echo-test\n    command: [echo, tested]\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "--json", "test", "demo"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("test failed code=%d out=%q err=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "\"name\": \"echo-test\"") || !strings.Contains(out.String(), "\"stdout\": \"tested\\n\"") {
		t.Fatalf("expected structured JSON test result, got %s", out.String())
	}
}

func TestPlainTestReturnsStructuredRows(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\ntests:\n  - name: echo-test\n    command: [sh, -c, 'printf \"line one\\nline two\\n\"']\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "--plain", "test", "demo"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("test failed code=%d out=%q err=%s", code, out.String(), errb.String())
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one TSV row, got %d: %q", len(lines), out.String())
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) != 6 || fields[0] != "echo-test" || fields[1] != "true" || fields[2] != "0" {
		t.Fatalf("unexpected plain test fields: %q", lines[0])
	}
	if strings.Contains(fields[4], "\n") || !strings.Contains(fields[4], "line one line two") {
		t.Fatalf("stdout field not sanitized: %q", fields[4])
	}
}

func TestRunHonorsNoInputAndManifestTimeout(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\ncommands:\n  - name: check-no-input\n    exec: [sh, -c, 'test \"$LONDONJOURNEYCLI_NO_INPUT\" = \"1\"']\n  - name: timeout\n    exec: [sh, -c, 'sleep 1']\n    timeout: 1ms\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--skills-dir", dir, "--no-input", "run", "demo", "check-no-input"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("no-input command failed code=%d out=%q err=%s", code, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"--skills-dir", dir, "run", "demo", "timeout"}, &out, &errb)
	if code != exitcode.Generic {
		t.Fatalf("timeout command code=%d out=%q err=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "timed out") {
		t.Fatalf("expected timeout diagnostic, got %s", errb.String())
	}
}
