package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func validateLocalOnlyRunFlags(cmd *cobra.Command) error {
	if err := validateConcurrency(cmd); err != nil {
		return err
	}
	return validatePhase(cmd)
}

func validateLocalTargets(cmd *cobra.Command) error {
	targets, _ := cmd.Flags().GetStringSlice("target")
	if len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 && isLocalTarget(targets[0]) {
		return nil
	}
	return fmt.Errorf("local-only mode only supports no --target or a single target of %q or %q", "local", "localhost")
}

func validateConcurrency(cmd *cobra.Command) error {
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	if concurrency >= 0 {
		return nil
	}
	return fmt.Errorf("--concurrency must be greater than or equal to 0")
}

func validatePhase(cmd *cobra.Command) error {
	phase, _ := cmd.Flags().GetString("phase")
	switch phase {
	case "", "plan", "fetch", "stage", "apply":
		return nil
	default:
		return fmt.Errorf("invalid --phase %q (want plan, fetch, stage, or apply)", phase)
	}
}

func commandContext(cmd *cobra.Command) (context.Context, context.CancelFunc, error) {
	base := cmd.Context()
	if base == nil {
		base = context.Background()
	}

	timeoutText, _ := cmd.Flags().GetString("timeout")
	if timeoutText == "" {
		ctx, cancel := context.WithCancel(base)
		return ctx, cancel, nil
	}

	timeout, err := time.ParseDuration(timeoutText)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --timeout %q: %w", timeoutText, err)
	}
	ctx, cancel := context.WithTimeout(base, timeout)
	return ctx, cancel, nil
}

func isLocalTarget(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "local", "localhost":
		return true
	default:
		return false
	}
}
