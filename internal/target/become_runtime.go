package target

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed become_windows_task.ps1
var becomeWindowsTaskScript string

func effectiveBecome(kind RuntimeKind, opts ExecutionOptions) (*BecomeOptions, error) {
	if !opts.Enabled() {
		return nil, nil
	}
	become := *opts.Become
	if strings.EqualFold(strings.TrimSpace(become.User), "SYSTEM") {
		return nil, fmt.Errorf("become: user %q is no longer supported; migrate to a privileged service account with credential elevation (become.method: runas)", become.User)
	}
	switch kind {
	case RuntimeKindWindowsPowerShell:
		if strings.TrimSpace(become.User) == "" {
			return nil, fmt.Errorf("become: user is required when enabled on Windows")
		}
		if become.Method == "" {
			become.Method = "runas"
		}
		if become.Method != "runas" {
			return nil, fmt.Errorf("become: unsupported Windows method %q", become.Method)
		}
	default:
		return nil, fmt.Errorf("become: unsupported runtime %q", kind)
	}
	return &become, nil
}

type windowsTaskBackend struct {
	run       func(context.Context, string, OutputFunc) (string, error)
	copyPlain func(context.Context, string, string) error
	tempDir   string
	become    *BecomeOptions
}

func (b *windowsTaskBackend) RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error) {
	return runWindowsPowerShellScript(ctx, b.run, b.tempDir, script, b.become, out)
}

func (b *windowsTaskBackend) CopyFile(ctx context.Context, src, dst string) error {
	if b.become == nil && b.copyPlain != nil {
		return b.copyPlain(ctx, src, dst)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	pathVar, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}
	payloadVar, err := powershellJSONVar("payload", base64.StdEncoding.EncodeToString(data))
	if err != nil {
		return err
	}
	_, err = runWindowsPowerShellScript(ctx, b.run, b.tempDir, pathVar+`
`+payloadVar+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String($payload))
`, b.become, nil)
	return err
}

func (b *windowsTaskBackend) RemoteTempDir() string {
	return b.tempDir
}

func runWindowsPowerShellScript(ctx context.Context, run func(context.Context, string, OutputFunc) (string, error), tempDir, script string, become *BecomeOptions, out OutputFunc) (string, error) {
	if become == nil {
		return run(ctx, script, out)
	}
	if strings.TrimSpace(become.Password) == "" {
		return "", fmt.Errorf("become: password is required for Windows user %q", become.User)
	}
	wrapped, err := buildWindowsCredentialRunner(tempDir, script, become)
	if err != nil {
		return "", err
	}
	return run(ctx, wrapped, out)
}

// buildWindowsCredentialRunner wraps a PowerShell payload so it executes as a
// different local/domain user over a remote transport (WinRM or SSH-to-Windows).
//
// It runs the payload through a one-shot scheduled task rather than
// CreateProcessWithLogonW (System.Diagnostics.Process with alternate
// credentials). A remote transport's session is non-interactive and has no
// window station, so CreateProcessWithLogonW fails child DLL initialization
// with STATUS_DLL_INIT_FAILED (0xC0000142). Task Scheduler runs the action in
// session 0 under its own service, which sidesteps the window-station
// requirement entirely.
//
// The generated script (executed as the connection account) grants the target
// user the batch-logon right, stages the payload, registers and runs the task
// as the target user with its password, waits for completion, replays the
// payload's stdout, and surfaces a non-zero exit code with the captured stderr.
//
// Note: profile loading follows the task's Password logon (a batch logon loads
// the user profile), so become.LoadProfile is not separately honored on this
// path.
func buildWindowsCredentialRunner(tempDir, payload string, become *BecomeOptions) (string, error) {
	tempVar, err := powershellJSONVar("tempRoot", filepath.ToSlash(tempDir))
	if err != nil {
		return "", err
	}
	payloadVar, err := powershellJSONVar("payload", payload)
	if err != nil {
		return "", err
	}
	userVar, err := powershellJSONVar("becomeUser", become.User)
	if err != nil {
		return "", err
	}
	passwordVar, err := powershellJSONVar("becomePassword", become.Password)
	if err != nil {
		return "", err
	}

	return tempVar + "\n" + payloadVar + "\n" + userVar + "\n" + passwordVar + "\n" + becomeWindowsTaskScript, nil
}
