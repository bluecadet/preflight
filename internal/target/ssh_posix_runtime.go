package target

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
)

type sshPOSIXShellRuntime struct {
	target           *SSHTarget
	powerShellBinary string
}

func (r *sshPOSIXShellRuntime) Kind() RuntimeKind {
	return RuntimeKindPOSIXShell
}

func (r *sshPOSIXShellRuntime) Registry() ModuleRegistry {
	return newPOSIXShellRegistry(r)
}

func (r *sshPOSIXShellRuntime) RunPOSIXCommand(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	return r.target.run(ctx, command, stdin)
}

func (r *sshPOSIXShellRuntime) CopyFile(ctx context.Context, src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf("mkdir -p %q && base64 -d > %q", shellDir(dst), dst)
	stdout, stderr, code, err := r.target.run(ctx, cmd, []byte(encoded))
	if err != nil {
		return wrapSSHTargetError(fmt.Sprintf("copy %q -> %q", src, dst), err)
	}
	if code != 0 {
		return wrapSSHTargetError(fmt.Sprintf("copy %q -> %q", src, dst), fmt.Errorf("exited with code %d: stdout=%q stderr=%q", code, strings.TrimSpace(stdout), strings.TrimSpace(stderr)))
	}
	fileMode := info.Mode().Perm()
	if info.Mode()&os.ModeSetuid != 0 {
		fileMode |= 0o4000
	}
	if info.Mode()&os.ModeSetgid != 0 {
		fileMode |= 0o2000
	}
	if info.Mode()&os.ModeSticky != 0 {
		fileMode |= 0o1000
	}
	mode := fmt.Sprintf("%04o", fileMode)
	chmodCmd := fmt.Sprintf("chmod %s %q", mode, dst)
	stdout, stderr, code, err = r.target.run(ctx, chmodCmd, nil)
	if err != nil {
		return wrapSSHTargetError(fmt.Sprintf("chmod %q", dst), err)
	}
	if code != 0 {
		return wrapSSHTargetError(fmt.Sprintf("chmod %q", dst), fmt.Errorf("exited with code %d: stdout=%q stderr=%q", code, strings.TrimSpace(stdout), strings.TrimSpace(stderr)))
	}
	return nil
}

func (r *sshPOSIXShellRuntime) ReadFile(ctx context.Context, path string) ([]byte, error) {
	stdout, _, code, err := r.target.run(ctx, fmt.Sprintf("base64 < %q", path), nil)
	if err != nil {
		return nil, wrapSSHTargetError(fmt.Sprintf("read %q", path), err)
	}
	if code != 0 {
		return nil, wrapSSHTargetError(fmt.Sprintf("read %q", path), fmt.Errorf("exited with code %d", code))
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
	if err != nil {
		return nil, wrapSSHTargetError(fmt.Sprintf("read %q", path), fmt.Errorf("decode remote file: %w", err))
	}
	return data, nil
}

func (r *sshPOSIXShellRuntime) Reachable(ctx context.Context) (bool, error) {
	_, _, code, err := r.target.run(ctx, "echo preflight", nil)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

func (r *sshPOSIXShellRuntime) Info(ctx context.Context) (TargetInfo, error) {
	stdout, _, code, err := r.target.run(ctx, "printf '%s|%s|%s\\n' \"$(hostname)\" \"$(uname -s)\" \"$(uname -m)\"", nil)
	if err != nil {
		return TargetInfo{}, wrapSSHTargetError("info", err)
	}
	if code != 0 {
		return TargetInfo{}, wrapSSHTargetError("info", fmt.Errorf("exited with code %d", code))
	}
	parts := strings.Split(strings.TrimSpace(stdout), "|")
	if len(parts) != 3 {
		return TargetInfo{}, wrapSSHTargetError("info", fmt.Errorf("unexpected output %q", stdout))
	}
	return TargetInfo{
		Hostname:  parts[0],
		OSVersion: parts[1],
		Arch:      parts[2],
		OSFamily:  normalizeOSFamily(parts[1]),
		Transport: r.target.Transport(),
	}, nil
}

func (r *sshPOSIXShellRuntime) RunPowerShellScript(ctx context.Context, script string, out OutputFunc) (string, error) {
	if r.powerShellBinary == "" {
		return "", fmt.Errorf("posix-shell runtime: powershell is not available on the remote host")
	}
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.powerShellBinary, script), nil)
	if err != nil {
		return "", wrapSSHTargetError("powershell failed", err)
	}
	if code != 0 {
		return "", wrapSSHTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
	}
	replayBatchOutput(stdout, out)
	return stdout, nil
}

func (r *sshPOSIXShellRuntime) PowerShellBinary() string {
	return r.powerShellBinary
}

func buildEncodedPowerShellCommand(binary, script string) string {
	encoded := encodePowerShellScript(script)
	return shellQuoteExec(binary, []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", encoded})
}

func encodePowerShellScript(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	buf := make([]byte, len(codeUnits)*2)
	for i, unit := range codeUnits {
		buf[2*i] = byte(unit)
		buf[2*i+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func shellDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func shellQuoteExec(cmd string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, fmt.Sprintf("%q", cmd))
	for _, arg := range args {
		parts = append(parts, fmt.Sprintf("%q", arg))
	}
	return strings.Join(parts, " ")
}

type sshClientRunner struct {
	client *ssh.Client
}

func (r *sshClientRunner) Close() error {
	return r.client.Close()
}

// NewSession opens a new multiplexed channel on the existing SSH connection.
// Implements sshSessionCreator to enable the persistent PowerShell session.
func (r *sshClientRunner) NewSession() (*ssh.Session, error) {
	return r.client.NewSession()
}

func (r *sshClientRunner) Run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", "", 0, err
	}
	defer func() {
		_ = session.Close()
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if stdin != nil {
		session.Stdin = bytes.NewReader(stdin)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return stdout.String(), stderr.String(), 0, ctx.Err()
	case err := <-errCh:
		if err == nil {
			return stdout.String(), stderr.String(), 0, nil
		}
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return stdout.String(), stderr.String(), exitErr.ExitStatus(), nil
		}
		return stdout.String(), stderr.String(), 0, err
	}
}
