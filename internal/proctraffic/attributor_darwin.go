//go:build darwin

package proctraffic

import (
	"context"
	"errors"
	"os/exec"
)

type nettopAttributor struct {
	command func(context.Context) (*exec.Cmd, error)
}

func NewNettopAttributor() Attributor {
	return nettopAttributor{
		command: func(ctx context.Context) (*exec.Cmd, error) {
			return exec.CommandContext(ctx, "nettop", nettopArgs()...), nil
		},
	}
}

func nettopArgs() []string {
	return []string{"-P", "-L", "0", "-x", "-n", "-d", "-s", "1"}
}

func (a nettopAttributor) Run(ctx context.Context, onSample func([]Sample)) error {
	cmd, err := a.command(ctx)
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	err = scanNettopCSV(ctx, stdout, onSample)
	waitErr := cmd.Wait()
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	if err != nil {
		return err
	}
	return waitErr
}
