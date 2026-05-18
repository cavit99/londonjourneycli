package notify

import (
	"context"
	"fmt"
	"os/exec"
)

type OpenClaw struct {
	Channel string
	Target  string
	DryRun  bool
}

func (o OpenClaw) Enabled() bool {
	return o.Channel != "" && o.Target != ""
}

func (o OpenClaw) Send(ctx context.Context, message string) error {
	if !o.Enabled() || o.DryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, "openclaw", "message", "send", "--channel", o.Channel, "--target", o.Target, "--message", message)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("openclaw message send: %w: %s", err, string(out))
	}
	return nil
}
