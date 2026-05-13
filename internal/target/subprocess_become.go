package target

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

// elevationWrapper spawns an elevated subprocess and returns a connected SDK client.
type elevationWrapper interface {
	Start(ctx context.Context, binary string, moduleName string) (*sdk.Client, error)
}

// subprocessModule implements target.Module by re-invoking the preflight binary
// under elevation via __module-exec.
type subprocessModule struct {
	name      string
	binary    string
	elevation elevationWrapper
}

func (m *subprocessModule) Check(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error) {
	cli, err := m.elevation.Start(ctx, m.binary, m.name)
	if err != nil {
		return CheckResult{}, fmt.Errorf("become: start elevated subprocess for %q check: %w", m.name, err)
	}
	defer cli.Close()

	var sdkOut sdk.OutputFunc
	if out != nil {
		sdkOut = sdk.OutputFunc(out)
	}
	sdkRes, err := cli.CheckStreaming(params, sdkOut)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: sdkRes.NeedsChange, Message: sdkRes.Message}, nil
}

func (m *subprocessModule) Apply(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
	cli, err := m.elevation.Start(ctx, m.binary, m.name)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("become: start elevated subprocess for %q apply: %w", m.name, err)
	}
	defer cli.Close()

	var sdkOut sdk.OutputFunc
	if out != nil {
		sdkOut = sdk.OutputFunc(out)
	}
	sdkRes, err := cli.ApplyStreaming(params, sdkOut)
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Message: sdkRes.Message}, nil
}

// posixSudoElevation spawns: sudo -S -p '' -u <user> <binary> __module-exec <module>
// feeding the password on stdin before the JSON-RPC frames.
type posixSudoElevation struct {
	user     string
	password string
}

func (e *posixSudoElevation) Start(ctx context.Context, binary string, moduleName string) (*sdk.Client, error) {
	var args []string
	if e.password != "" {
		args = []string{"-S", "-p", "", "-u", e.user, binary, "__module-exec", moduleName}
	} else {
		args = []string{"-u", e.user, binary, "__module-exec", moduleName}
	}
	cmd := exec.CommandContext(ctx, "sudo", args...)
	if e.password != "" {
		cmd.Stdin = strings.NewReader(e.password + "\n")
	}
	return sdk.NewClientFromCmd(cmd)
}

// windowsCredentialElevation spawns the binary under a different user account
// using System.Diagnostics.Process (not Start-Process) which supports
// stdin/stdout redirection with credentials.
type windowsCredentialElevation struct {
	user        string
	password    string
	loadProfile bool
}

func (e *windowsCredentialElevation) Start(ctx context.Context, binary string, moduleName string) (*sdk.Client, error) {
	// Use a PowerShell wrapper that uses System.Diagnostics.Process to spawn
	// the binary under alternate credentials with pipe-based stdio.
	loadProfileStr := "false"
	if e.loadProfile {
		loadProfileStr = "true"
	}

	script := buildWindowsElevatedSubprocessWrapper(binary, moduleName, e.user, e.password, loadProfileStr)
	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	)
	return sdk.NewClientFromCmd(cmd)
}

func buildWindowsElevatedSubprocessWrapper(binary, moduleName, user, password, loadProfile string) string {
	// Escape values for PowerShell string literals
	escapedBinary := strings.ReplaceAll(binary, "'", "''")
	escapedModule := strings.ReplaceAll(moduleName, "'", "''")
	escapedUser := strings.ReplaceAll(user, "'", "''")
	escapedPassword := strings.ReplaceAll(password, "'", "''")

	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = '%s'
$psi.Arguments = '__module-exec %s'
$psi.UseShellExecute = $false
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UserName = '%s'
$psi.Password = (ConvertTo-SecureString '%s' -AsPlainText -Force)
$psi.LoadUserProfile = $%s
$proc = New-Object System.Diagnostics.Process
$proc.StartInfo = $psi
[void]$proc.Start()
$stdinWriter = $proc.StandardInput
$stdoutReader = $proc.StandardOutput
$stderrReader = $proc.StandardError
$inputTask = [System.Console]::In.ReadToEndAsync()
$writeTask = $stdinWriter.WriteAsync($inputTask.Result)
$outputTask = $stdoutReader.ReadToEndAsync()
$proc.WaitForExit()
$writeTask.Wait()
$outputTask.Wait()
Write-Host -NoNewline $outputTask.Result
exit $proc.ExitCode
`, escapedBinary, escapedModule, escapedUser, escapedPassword, loadProfile)
}

// newSubprocessBecomeRegistry wraps each module in the source registry as a
// subprocessModule configured with the appropriate elevation for the runtime.
func newSubprocessBecomeRegistry(source ModuleRegistry, kind RuntimeKind, become *BecomeOptions) (ModuleRegistry, error) {
	binary, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("become: resolve binary path: %w", err)
	}

	var elevation elevationWrapper
	switch kind {
	case RuntimeKindPOSIXShell:
		elevation = &posixSudoElevation{
			user:     become.User,
			password: become.Password,
		}
	case RuntimeKindWindowsPowerShell:
		loadProfile := true
		if become.LoadProfile != nil {
			loadProfile = *become.LoadProfile
		}
		elevation = &windowsCredentialElevation{
			user:        become.User,
			password:    become.Password,
			loadProfile: loadProfile,
		}
	default:
		return nil, fmt.Errorf("become: unsupported runtime %q for subprocess elevation", kind)
	}

	reg := make(ModuleRegistry, len(source))
	for name, mod := range source {
		_ = mod // source module exists; wrap it in subprocess
		reg[name] = &subprocessModule{
			name:      name,
			binary:    binary,
			elevation: elevation,
		}
	}
	return reg, nil
}
