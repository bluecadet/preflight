package target

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type sshWindowsPowerShellRuntime struct {
	target      *SSHTarget
	binary      string
	psSessionMu sync.Mutex
	psSession   *sshPersistentPS
}

// sshPersistentPS holds a single long-running PowerShell process started inside
// a reused SSH channel. All Check/Apply scripts are serialised through it,
// eliminating per-task powershell.exe startup overhead (~200–500 ms each).
type sshPersistentPS struct {
	session *ssh.Session
	stdin   io.WriteCloser
	reader  *bufio.Reader
	mu      sync.Mutex
}

func (p *sshPersistentPS) run(_ context.Context, script string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := generateSessionID()
	line := buildPSStdinLine(script, id) + "\n"
	if _, err := p.stdin.Write([]byte(line)); err != nil {
		return "", &psSessionError{fmt.Errorf("write stdin: %w", err)}
	}
	return readPSOutput(p.reader, id)
}

func (p *sshPersistentPS) close() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.session != nil {
		// Wait for the remote PowerShell process to notice stdin EOF and exit.
		_ = p.session.Wait()
		_ = p.session.Close()
	}
}

func (r *sshWindowsPowerShellRuntime) Kind() RuntimeKind {
	return RuntimeKindWindowsPowerShell
}

func (r *sshWindowsPowerShellRuntime) Registry() remoteModuleRegistry {
	return newWindowsPowerShellRegistry(r)
}

// getOrCreatePSSession returns the cached persistent PS session, creating it on
// first call. Returns nil (without error) when the underlying runner does not
// implement sshSessionCreator (e.g. test fakes), in which case the caller falls
// back to per-command execution.
func (r *sshWindowsPowerShellRuntime) getOrCreatePSSession(ctx context.Context) (*sshPersistentPS, error) {
	_ = ctx
	r.psSessionMu.Lock()
	defer r.psSessionMu.Unlock()
	if r.psSession != nil {
		return r.psSession, nil
	}

	runner, err := r.target.clientRunner()
	if err != nil {
		return nil, err
	}
	creator, ok := runner.(sshSessionCreator)
	if !ok {
		return nil, nil // runner doesn't support raw sessions; use legacy path
	}

	session, err := creator.NewSession()
	if err != nil {
		return nil, wrapSSHTargetError("create persistent powershell session", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, wrapSSHTargetError("persistent powershell stdin pipe", err)
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = session.Close()
		return nil, wrapSSHTargetError("persistent powershell stdout pipe", err)
	}

	// Start PowerShell in stdin-reading mode. -Command - causes PS to read
	// and execute commands from stdin until EOF, acting as a persistent REPL.
	cmd := shellQuoteExec(r.binary, []string{"-NoProfile", "-NonInteractive", "-Command", "-"})
	if err := session.Start(cmd); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		return nil, wrapSSHTargetError("start persistent powershell", err)
	}

	r.psSession = &sshPersistentPS{session: session, stdin: stdin, reader: bufio.NewReader(stdoutPipe)}
	return r.psSession, nil
}

func (r *sshWindowsPowerShellRuntime) resetPSSession() {
	r.psSessionMu.Lock()
	defer r.psSessionMu.Unlock()
	if r.psSession != nil {
		r.psSession.close()
		r.psSession = nil
	}
}

func (r *sshWindowsPowerShellRuntime) Close() error {
	r.resetPSSession()
	return nil
}

// RunPowerShellScript executes a PowerShell script on the remote Windows host.
// It first tries the persistent session (one long-lived powershell.exe per
// target), which eliminates per-task process-startup overhead. If the session
// cannot be created or signals a transport failure, it falls back to
// runPSLegacy which spawns a fresh PowerShell process per invocation.
func (r *sshWindowsPowerShellRuntime) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	return runPSWithFallback(ctx, script,
		func(ctx context.Context) (psSessionRunner, error) {
			ps, err := r.getOrCreatePSSession(ctx)
			if ps == nil {
				return nil, err
			}
			return ps, err
		},
		func(error) { r.resetPSSession() },
		r.runPSLegacy,
	)
}

func (r *sshWindowsPowerShellRuntime) runPSLegacy(ctx context.Context, script string) (string, error) {
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.binary, script), nil)
	if err != nil {
		return "", wrapSSHTargetError("powershell failed", err)
	}
	if code != 0 {
		return "", wrapSSHTargetError("powershell failed", fmt.Errorf("exited with code %d: %s", code, strings.TrimSpace(stderr)))
	}
	return stdout, nil
}

func (r *sshWindowsPowerShellRuntime) CopyFile(ctx context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	script, err := powershellJSONVar("path", dst)
	if err != nil {
		return err
	}
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.binary, script+`
$dir = Split-Path -Parent $path
if ($dir) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
}
$payload = [Console]::In.ReadToEnd()
[IO.File]::WriteAllBytes($path, [Convert]::FromBase64String($payload))
`), []byte(encoded))
	if err != nil {
		return wrapSSHTargetError(fmt.Sprintf("copy %q -> %q", src, dst), err)
	}
	if code != 0 {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		return wrapSSHTargetError(fmt.Sprintf("copy %q -> %q", src, dst), fmt.Errorf("exited with code %d: %s", code, message))
	}
	return nil
}

func (r *sshWindowsPowerShellRuntime) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return readRemoteWindowsFile(ctx, r.target.Transport(), r.RunPowerShellScript, path)
}

func (r *sshWindowsPowerShellRuntime) Reachable(ctx context.Context) (bool, error) {
	stdout, err := r.RunPowerShellScript(ctx, `Write-Output 'preflight'`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == "preflight", nil
}

func (r *sshWindowsPowerShellRuntime) Info(ctx context.Context) (TargetInfo, error) {
	return remoteWindowsTargetInfo(ctx, r.target.Transport(), r.RunPowerShellScript)
}

func (r *sshWindowsPowerShellRuntime) RemoteTempDir() string {
	return windowsRemoteTempDir
}
