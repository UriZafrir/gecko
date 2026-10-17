// Package execx starts the helper programs Gecko shells out to (git, ssh,
// tmux, ...) without putting a console window on screen.
package execx

import (
	"context"
	"os/exec"
)

// Command is exec.Command for a helper the daemon runs in the background.
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(resolve(name), arg...)
	hide(cmd)
	return cmd
}

// CommandContext is exec.CommandContext for such a helper.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, resolve(name), arg...)
	hide(cmd)
	return cmd
}
