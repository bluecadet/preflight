package target

import (
	"context"
	"fmt"
)

// SSHTarget communicates with a remote machine over SSH.
// All methods are stubs pending full implementation.
type SSHTarget struct {
	Host       string
	Port       int
	Username   string
	PrivateKey string
}

func (t *SSHTarget) Execute(_ context.Context, _ string, _ string, _ map[string]interface{}, _ bool) (Result, error) {
	return Result{}, fmt.Errorf("ssh: not yet implemented")
}

func (t *SSHTarget) CopyFile(_ context.Context, _, _ string) error {
	return fmt.Errorf("ssh: not yet implemented")
}

func (t *SSHTarget) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("ssh: not yet implemented")
}

func (t *SSHTarget) Reachable(_ context.Context) (bool, error) {
	return false, fmt.Errorf("ssh: not yet implemented")
}

func (t *SSHTarget) Info(_ context.Context) (TargetInfo, error) {
	return TargetInfo{}, fmt.Errorf("ssh: not yet implemented")
}
