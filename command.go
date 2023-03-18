package smush

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/fatih/color"

	"golang.org/x/sync/semaphore"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Commands []*Command `yaml:"commands"`
}

type Command struct {
	Name string `yaml:"name"`
	Runs string `yaml:"runs"`

	cmd *exec.Cmd
}

func ReadConfig(r io.Reader) (*Config, error) {
	data := &Config{}
	if err := yaml.NewDecoder(r).Decode(data); err != nil {
		return nil, fmt.Errorf("decoding yaml: %w", err)
	}
	return data, nil
}

func (c *Command) Run(ctx context.Context, stdout, stderr io.Writer) error {
	cmdArgs := strings.Split(c.Runs, " ")
	c.cmd = exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	c.cmd.Stdout = stdout
	c.cmd.Stderr = stderr

	err := c.cmd.Run()
	exitCode := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			exitCode = -99
		} else {
			exitCode = exitErr.ExitCode()
		}
	}

	if stderr != nil {
		str := color.New(color.Bold)
		str.Fprintf(stderr, ">>> exited %d\n", exitCode)
	}

	if err != nil {
		return fmt.Errorf("running command '%s': %w", c.Runs, err)
	}
	return nil
}

func RunAll(ctx context.Context, maxProcs int64, commands []*Command) error {
	throttle := semaphore.NewWeighted(maxProcs)
	errors := make(chan error, len(commands))
	hasError := false

	for _, command := range commands {
		throttle.Acquire(ctx, 1)
		go func(cmd *Command) {
			errors <- cmd.Run(ctx, os.Stdout, os.Stderr)
			throttle.Release(1)
		}(command)
	}
	go func() {
		// Ensure all processes have exited by acquiring maximum weight of semaphore...
		throttle.Acquire(context.Background(), maxProcs)
		// ...before finally closing the channel, so the channel receiver doesn't terminate early.
		close(errors)
	}()
	for err := range errors {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			hasError = true
		}
	}

	if hasError {
		return fmt.Errorf("at least one command exited with error")
	}
	return nil
}
