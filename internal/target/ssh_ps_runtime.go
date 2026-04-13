package target

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
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
	scanner *bufio.Scanner
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
	return readPSOutput(p.scanner, id)
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
		return nil, fmt.Errorf("ssh: create persistent PS session: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("ssh: persistent PS stdin pipe: %w", err)
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = session.Close()
		return nil, fmt.Errorf("ssh: persistent PS stdout pipe: %w", err)
	}

	// Start PowerShell in stdin-reading mode. -Command - causes PS to read
	// and execute commands from stdin until EOF, acting as a persistent REPL.
	cmd := shellQuoteExec(r.binary, []string{"-NoProfile", "-NonInteractive", "-Command", "-"})
	if err := session.Start(cmd); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		return nil, fmt.Errorf("ssh: start persistent powershell: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB per line; handles large module output
	r.psSession = &sshPersistentPS{session: session, stdin: stdin, scanner: scanner}
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

// RunPowerShellScript executes a PowerShell script on the remote Windows host.
// It first tries the persistent session (one long-lived powershell.exe per
// target), which eliminates per-task process-startup overhead. If the session
// cannot be created or signals a transport failure, it falls back to
// runPSLegacy which spawns a fresh PowerShell process per invocation.
func (r *sshWindowsPowerShellRuntime) RunPowerShellScript(ctx context.Context, script string) (string, error) {
	ps, err := r.getOrCreatePSSession(ctx)
	if err == nil && ps != nil {
		out, psErr := ps.run(ctx, script)
		if psErr == nil {
			return out, nil
		}
		if isSessionError(psErr) {
			r.resetPSSession()
		} else {
			return out, psErr
		}
	}
	return r.runPSLegacy(ctx, script)
}

func (r *sshWindowsPowerShellRuntime) runPSLegacy(ctx context.Context, script string) (string, error) {
	stdout, stderr, code, err := r.target.run(ctx, buildEncodedPowerShellCommand(r.binary, script), nil)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("ssh powershell exited with code %d: %s", code, strings.TrimSpace(stderr))
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
		return err
	}
	if code != 0 {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		return fmt.Errorf("ssh copy exited with code %d: %s", code, message)
	}
	return nil
}

func (r *sshWindowsPowerShellRuntime) ReadFile(ctx context.Context, path string) ([]byte, error) {
	script, err := powershellJSONVar("path", path)
	if err != nil {
		return nil, err
	}
	stdout, err := r.RunPowerShellScript(ctx, script+`
if (-not (Test-Path -LiteralPath $path)) {
  throw "file not found: $path"
}
[Convert]::ToBase64String([IO.File]::ReadAllBytes($path))
`)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
}

func (r *sshWindowsPowerShellRuntime) Reachable(ctx context.Context) (bool, error) {
	stdout, err := r.RunPowerShellScript(ctx, `Write-Output 'preflight'`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(stdout) == "preflight", nil
}

func (r *sshWindowsPowerShellRuntime) Info(ctx context.Context) (TargetInfo, error) {
	stdout, err := r.RunPowerShellScript(ctx, `
	$os = Get-CimInstance Win32_OperatingSystem
	$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
	[pscustomobject]@{
	  hostname = $env:COMPUTERNAME
	  version  = [string]$os.Version
  build    = [string]$os.BuildNumber
  arch     = $arch
} | ConvertTo-Json -Compress
`)
	if err != nil {
		return TargetInfo{}, err
	}
	var payload struct {
		Hostname string `json:"hostname"`
		Version  string `json:"version"`
		Build    string `json:"build"`
		Arch     string `json:"arch"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return TargetInfo{}, fmt.Errorf("ssh: parse target info: %w", err)
	}
	return TargetInfo{
		Hostname:  payload.Hostname,
		OSVersion: payload.Version,
		OSBuild:   payload.Build,
		Arch:      normalizeWindowsArch(payload.Arch),
		OSFamily:  OSFamilyWindows,
		Transport: r.target.Transport(),
	}, nil
}

func (r *sshWindowsPowerShellRuntime) RemoteTempDir() string {
	return `C:\Windows\Temp\preflight`
}
