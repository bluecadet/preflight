package target

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// localPowerShellBackend runs module scripts through powershell.exe on the
// controller machine. It satisfies windowsPowerShellBackend so LocalTarget
// reuses the same module registry as the remote Windows transports.
type localPowerShellBackend struct{}

func (localPowerShellBackend) RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error) {
	// Mirror the persistent remote session: errors that escape the script fail
	// the run. Under -File they would otherwise print and exit 0.
	wrapped := "$ProgressPreference='SilentlyContinue'\ntry {\n& {\n" + script + "\n}\n} catch {\n[Console]::Error.WriteLine($_.ToString())\nexit 1\n}\n"
	cmd, cleanup, err := localPowerShellCommand(ctx, wrapped)
	if err != nil {
		return "", err
	}
	defer cleanup()

	sw := &lineStreamWriter{out: out}
	var stderr bytes.Buffer
	cmd.Stdout = sw
	cmd.Stderr = &stderr
	err = cmd.Run()
	sw.flush()
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) {
		return "", wrapLocalTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr.String())))
	}
	if err != nil {
		return "", wrapLocalTargetError("powershell failed", err)
	}
	return strings.ReplaceAll(sw.all.String(), "\r\n", "\n"), nil
}

func (localPowerShellBackend) CopyFile(_ context.Context, src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return wrapLocalTargetError(fmt.Sprintf("stat src %q", src), err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return wrapLocalTargetError(fmt.Sprintf("read src %q", src), err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapLocalTargetError(fmt.Sprintf("create dir for %q", dst), err)
	}
	if err := os.WriteFile(dst, data, info.Mode()); err != nil {
		return wrapLocalTargetError(fmt.Sprintf("write dst %q", dst), err)
	}
	return nil
}

func (localPowerShellBackend) RemoteTempDir() string { return windowsRemoteTempDir }

// localPowerShellCommand runs script from a temp .ps1 via -File. -File keeps
// stderr as plain text (-EncodedCommand emits CLIXML when stderr is
// redirected) and has no command-line length limit. The returned cleanup
// removes the temp file.
func localPowerShellCommand(ctx context.Context, script string) (*exec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "preflight-*.ps1")
	if err != nil {
		return nil, nil, wrapLocalTargetError("stage script", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	// A UTF-8 BOM makes Windows PowerShell 5.1 read the file as UTF-8.
	_, writeErr := f.WriteString("\ufeff" + script)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		cleanup()
		return nil, nil, wrapLocalTargetError("stage script", err)
	}
	return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", f.Name()), cleanup, nil
}
