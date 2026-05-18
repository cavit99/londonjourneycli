package notify

import (
	"context"
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
