package notify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDryRun(t *testing.T) {
	n := OpenClaw{Channel: "whatsapp", Target: "+10000000000", DryRun: true}
	if !n.Enabled() {
		t.Fatal("expected enabled sender")
	}
	if err := n.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenClawDisabled(t *testing.T) {
	n := OpenClaw{}
	if n.Enabled() {
		t.Fatal("expected disabled sender")
	}
	if err := n.Send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenClawSendInvokesCLI(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.log")
	bin := filepath.Join(dir, "openclaw")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LONDONJOURNEYCLI_OPENCLAW_ARGS\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LONDONJOURNEYCLI_OPENCLAW_ARGS", logPath)

	n := OpenClaw{Channel: "whatsapp", Target: "+10000000000"}
	if err := n.Send(context.Background(), "hello there"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"message", "send", "--channel", "whatsapp", "--target", "+10000000000", "--message", "hello there"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("args=%q want %q", got, want)
	}
}

func TestOpenClawSendReturnsCLIOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho nope >&2\nexit 12\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := (OpenClaw{Channel: "whatsapp", Target: "+10000000000"}).Send(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected command output in error, got %v", err)
	}
}
