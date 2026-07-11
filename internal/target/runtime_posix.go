package target

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

type posixShellBackend interface {
	powerShellScriptBackend
	RunPOSIXCommand(ctx context.Context, command string, stdin []byte) (stdout string, stderr string, exitCode int, err error)
	PackageManager(ctx context.Context) (string, error)
	CopyFile(ctx context.Context, src, dst string) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	PowerShellBinary() string
	// Probe returns the cached POSIX runtime detection signals (init system,
	// package manager) for the target. POSIX modules whose behavior depends on
	// these signals (reboot if_needed, wait service_running) read them here
	// rather than re-probing, so there is one detection path per target.
	Probe(ctx context.Context) (Probe, error)
}

// newPOSIXShellRegistry builds a ModuleRegistry for remote POSIX targets (SSH-POSIX).
// LocalTarget no longer calls this function — local become uses subprocess elevation
// via newSubprocessBecomeRegistry instead.
func newPOSIXShellRegistry(backend posixShellBackend) ModuleRegistry {
	supported := ModuleRegistry{
		"directory": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXDirectory(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyPOSIXDirectory(ctx, backend, params)
			},
		},
		"file": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXFile(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyPOSIXFile(ctx, backend, params)
			},
		},
		"shell": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXShell(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyPOSIXShell(ctx, backend, params, out)
			},
		},
		"wait": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXWait(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyPOSIXWait(ctx, backend, params)
			},
		},
		"user": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXUser(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return ApplyResult{}, applyPOSIXUser(ctx, backend, params)
			},
		},
		"reboot": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXReboot(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, _ OutputFunc) (ApplyResult, error) {
				return applyPOSIXReboot(ctx, backend, params)
			},
		},
		"system_package": moduleFuncs{
			check: func(ctx context.Context, params map[string]any, _ OutputFunc) (CheckResult, error) {
				return checkPOSIXSystemPackage(ctx, backend, params)
			},
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyPOSIXSystemPackage(ctx, backend, params, out)
			},
		},
	}
	if backend.PowerShellBinary() != "" {
		supported["powershell"] = moduleFuncs{
			check: func(ctx context.Context, params map[string]any, out OutputFunc) (CheckResult, error) {
				return checkPowerShellModuleWithOutput(ctx, backend, params, out)
			},
			// applyPowerShellModule streams lines through out during execution.
			// Pass nil to applyStreamed so it only extracts a single-line message
			// without re-emitting lines that were already forwarded.
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				return applyPowerShellModule(ctx, backend, params, out)
			},
		}
	}
	return buildRemoteModuleRegistry(RuntimeKindPOSIXShell, supported, func(module string) error {
		switch module {
		case "powershell":
			return NewMissingPrerequisiteError(module, RuntimeKindPOSIXShell, "requires pwsh or powershell on the remote host")
		default:
			return NewUnsupportedOnRuntimeError(module, RuntimeKindPOSIXShell)
		}
	})
}

func checkPOSIXDirectory(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return CheckResult{}, fmt.Errorf("directory: required param %q is missing", "path")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}

	switch ensure {
	case "absent":
		return posixNonZeroExitMeansChange(ctx, backend, fmt.Sprintf("test ! -e %q", path))
	case "present":
		stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("if [ ! -e %q ]; then printf missing; elif [ -d %q ]; then printf dir; else printf other; fi", path, path), nil)
		if err != nil {
			return CheckResult{}, err
		}
		if code != 0 {
			return CheckResult{}, fmt.Errorf("directory check exited with code %d: %s", code, strings.TrimSpace(stderr))
		}
		switch strings.TrimSpace(stdout) {
		case "missing":
			return CheckResult{NeedsChange: true}, nil
		case "dir":
			return CheckResult{}, nil
		default:
			return CheckResult{}, fmt.Errorf("directory: %q exists but is not a directory", path)
		}
	default:
		return CheckResult{}, fmt.Errorf("directory: unknown ensure value %q (want present|absent)", ensure)
	}
}

func applyPOSIXDirectory(ctx context.Context, backend posixShellBackend, params map[string]any) error {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("directory: required param %q is missing", "path")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	switch ensure {
	case "absent":
		return posixMustRun(ctx, backend, fmt.Sprintf("rm -rf %q", path))
	case "present":
		return posixMustRun(ctx, backend, fmt.Sprintf("mkdir -p %q", path))
	default:
		return fmt.Errorf("directory: unknown ensure value %q (want present|absent)", ensure)
	}
}

func checkPOSIXFile(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return CheckResult{}, fmt.Errorf("file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)
	content, hasContent, err := fileContentParam(params, "file", src)
	if err != nil {
		return CheckResult{}, err
	}

	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("if [ ! -e %q ]; then printf missing; elif [ -d %q ]; then printf dir; else printf file; fi", dest, dest), nil)
	if err != nil {
		return CheckResult{}, err
	}
	if code != 0 {
		return CheckResult{}, fmt.Errorf("file check exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	state := strings.TrimSpace(stdout)

	switch ensure {
	case "absent":
		switch state {
		case "dir":
			return CheckResult{}, fmt.Errorf("file module cannot remove directory %q: use the directory module with ensure:absent instead", dest)
		case "file":
			return CheckResult{NeedsChange: true, Message: "file exists, will remove"}, nil
		default: // "missing"
			return CheckResult{}, nil
		}
	case "present":
		switch state {
		case "missing":
			return CheckResult{NeedsChange: true}, nil
		case "dir":
			return CheckResult{}, fmt.Errorf("file: %q is a directory, not a file", dest)
		case "file":
			if src == "" {
				if !hasContent {
					return CheckResult{}, nil
				}
			}
			if hasContent {
				remoteHash, err := posixRemoteFileHash(ctx, backend, dest)
				if err != nil {
					return CheckResult{}, err
				}
				return CheckResult{NeedsChange: hashBytes([]byte(content)) != remoteHash}, nil
			}
			localHash, err := hashLocalFile(src)
			if err != nil {
				return CheckResult{}, err
			}
			remoteHash, err := posixRemoteFileHash(ctx, backend, dest)
			if err != nil {
				return CheckResult{}, err
			}
			return CheckResult{NeedsChange: localHash != remoteHash}, nil
		default:
			return CheckResult{}, fmt.Errorf("file: unexpected remote state %q", state)
		}
	default:
		return CheckResult{}, fmt.Errorf("file: unknown ensure value %q (want present|absent)", ensure)
	}
}

func applyPOSIXFile(ctx context.Context, backend posixShellBackend, params map[string]any) error {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return fmt.Errorf("file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)
	content, hasContent, err := fileContentParam(params, "file", src)
	if err != nil {
		return err
	}

	switch ensure {
	case "absent":
		return posixMustRun(ctx, backend, fmt.Sprintf("rm -f %q", dest))
	case "present":
		if src != "" {
			return backend.CopyFile(ctx, src, dest)
		}
		if hasContent {
			return posixMustRunWithStdin(ctx, backend, fmt.Sprintf("mkdir -p %q && cat > %q", shellDir(dest), dest), []byte(content))
		}
		return posixMustRun(ctx, backend, fmt.Sprintf("mkdir -p %q && : > %q", shellDir(dest), dest))
	default:
		return fmt.Errorf("file: unknown ensure value %q (want present|absent)", ensure)
	}
}

func checkPOSIXShell(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	creates, _ := params["creates"].(string)
	if creates == "" {
		return CheckResult{NeedsChange: true}, nil
	}
	workingDir, _ := params["working_dir"].(string)
	if workingDir != "" {
		return posixNonZeroExitMeansChange(ctx, backend, fmt.Sprintf("cd %q && test -e %q", workingDir, creates))
	}
	return posixNonZeroExitMeansChange(ctx, backend, fmt.Sprintf("test -e %q", creates))
}

func applyPOSIXShell(ctx context.Context, backend posixShellBackend, params map[string]any, out OutputFunc) (ApplyResult, error) {
	cmd, ok := params["cmd"].(string)
	if !ok || cmd == "" {
		return ApplyResult{}, fmt.Errorf("shell: required param %q is missing", "cmd")
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return ApplyResult{}, err
	}
	workingDir, _ := params["working_dir"].(string)

	var shellCmd strings.Builder
	if workingDir != "" {
		fmt.Fprintf(&shellCmd, "cd %q && ", workingDir)
	}
	shellCmd.WriteString(shellQuoteExec(cmd, args))
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, shellCmd.String(), nil)
	if err != nil {
		return ApplyResult{}, err
	}
	if code != 0 {
		return ApplyResult{}, fmt.Errorf("posix command exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return applyStreamed(stdout, out), nil
}

func checkPOSIXWait(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	met, err := posixWaitCondition(ctx, backend, condition, targetValue)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: !met}, nil
}

func applyPOSIXWait(ctx context.Context, backend posixShellBackend, params map[string]any) error {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	timeoutStr, _ := params["timeout"].(string)
	if timeoutStr == "" {
		timeoutStr = "5m"
	}
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return fmt.Errorf("wait: invalid timeout %q: %w", timeoutStr, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		met, err := posixWaitCondition(ctx, backend, condition, targetValue)
		if err != nil {
			return err
		}
		if met {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait: timeout after %s waiting for condition %q on %q", timeoutStr, condition, targetValue)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func posixWaitCondition(ctx context.Context, backend posixShellBackend, condition, targetValue string) (bool, error) {
	switch condition {
	case "file_exists":
		_, _, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("test -e %q", targetValue), nil)
		if err != nil {
			return false, err
		}
		return code == 0, nil
	case "port_open":
		return posixPortOpen(ctx, backend, targetValue)
	case "service_running":
		return posixServiceRunning(ctx, backend, targetValue)
	default:
		return false, fmt.Errorf("wait: unknown condition %q (want port_open|file_exists|service_running)", condition)
	}
}

// posixServiceRunning checks whether a systemd service is active via
// `systemctl is-active --quiet`. It requires the init signal to be systemd;
// an empty init signal fails per-task with the typed environment-prerequisite
// error so the run log carries the missing_prerequisite reason code.
func posixServiceRunning(ctx context.Context, backend posixShellBackend, name string) (bool, error) {
	probe, err := backend.Probe(ctx)
	if err != nil {
		return false, err
	}
	if probe.Init != "systemd" {
		return false, NewMissingPrerequisiteError("wait", RuntimeKindPOSIXShell,
			"service_running requires systemd; no init system detected on the target")
	}
	_, _, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("systemctl is-active --quiet %q", name), nil)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

func posixPortOpen(ctx context.Context, backend posixShellBackend, targetValue string) (bool, error) {
	host, port, ok := strings.Cut(targetValue, ":")
	if !ok || host == "" || port == "" {
		return false, fmt.Errorf("wait: port_open target must be host:port")
	}
	command := fmt.Sprintf(`
if command -v nc >/dev/null 2>&1; then
  nc -z %q %q >/dev/null 2>&1
elif command -v python3 >/dev/null 2>&1; then
  python3 -c 'import socket,sys;s=socket.socket();s.settimeout(2);rc=s.connect_ex((sys.argv[1],int(sys.argv[2])));s.close();sys.exit(0 if rc == 0 else 1)' %q %q
elif command -v python >/dev/null 2>&1; then
  python -c 'import socket,sys;s=socket.socket();s.settimeout(2);rc=s.connect_ex((sys.argv[1],int(sys.argv[2])));s.close();sys.exit(0 if rc == 0 else 1)' %q %q
elif command -v perl >/dev/null 2>&1; then
  perl -MIO::Socket::INET -e 'my ($h,$p)=@ARGV; my $s = IO::Socket::INET->new(PeerAddr=>$h, PeerPort=>$p, Proto=>"tcp", Timeout=>2); exit($s ? 0 : 1);' %q %q
elif command -v bash >/dev/null 2>&1; then
  bash -lc 'exec 3<>/dev/tcp/$0/$1' %q %q >/dev/null 2>&1
else
  echo "no supported TCP probe tool found" >&2
  exit 127
fi
`, host, port, host, port, host, port, host, port, host, port)
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, command, nil)
	if err != nil {
		return false, err
	}
	if code == 0 {
		return true, nil
	}
	if code == 127 {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = strings.TrimSpace(stdout)
		}
		if message == "" {
			message = "no supported TCP probe tool found"
		}
		return false, fmt.Errorf("wait: port_open probe unavailable: %s", message)
	}
	return false, nil
}

func posixNonZeroExitMeansChange(ctx context.Context, backend posixShellBackend, command string) (CheckResult, error) {
	_, stderr, code, err := backend.RunPOSIXCommand(ctx, command, nil)
	if err != nil {
		return CheckResult{}, err
	}
	if code == 0 {
		return CheckResult{}, nil
	}
	if stderr != "" {
		return CheckResult{}, fmt.Errorf("check command failed: %s", strings.TrimSpace(stderr))
	}
	// A non-zero exit with no stderr is treated as "condition not met" rather
	// than an error. This is only safe for test(1) commands where exit code 1
	// unambiguously means the condition is false, not a shell or command failure.
	return CheckResult{NeedsChange: true}, nil
}

func posixMustRun(ctx context.Context, backend posixShellBackend, command string) error {
	return posixMustRunWithStdin(ctx, backend, command, nil)
}

func posixMustRunWithStdin(ctx context.Context, backend posixShellBackend, command string, stdin []byte) error {
	_, stderr, code, err := backend.RunPOSIXCommand(ctx, command, stdin)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("posix command exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return nil
}

func posixRemoteFileHash(ctx context.Context, backend posixShellBackend, path string) (string, error) {
	for _, command := range []string{
		fmt.Sprintf("sha256sum %q", path),
		fmt.Sprintf("shasum -a 256 %q", path),
	} {
		stdout, _, code, err := backend.RunPOSIXCommand(ctx, command, nil)
		if err != nil {
			return "", err
		}
		if code != 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(stdout))
		if len(fields) == 0 {
			return "", fmt.Errorf("file: unable to parse hash output for %q", path)
		}
		return strings.ToLower(fields[0]), nil
	}

	data, err := backend.ReadFile(ctx, path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

// --- reboot ----------------------------------------------------------------

// checkPOSIXReboot decides whether a reboot is needed. condition "always"
// always reports a needed change. condition "if_needed" (the default) probes
// the distro-appropriate reboot-required signal, driven by the cached package
// manager detection: apt → /var/run/reboot-required marker file; dnf →
// `needs-restarting -r` (exit 1 = reboot required). When neither package
// manager is detected, no reboot signal is available and Check reports no
// change with a message stating the situation. Both conditions require
// systemd (the reboot path uses systemctl); an empty init signal fails
// per-task with the typed environment-prerequisite error.
func checkPOSIXReboot(ctx context.Context, backend posixShellBackend, params map[string]any) (CheckResult, error) {
	condition, _ := params["condition"].(string)
	if condition == "" {
		condition = "if_needed"
	}

	probe, err := backend.Probe(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	if probe.Init != "systemd" {
		return CheckResult{}, NewMissingPrerequisiteError("reboot", RuntimeKindPOSIXShell,
			"requires systemd; no init system detected on the target")
	}

	if condition == "always" {
		return CheckResult{NeedsChange: true}, nil
	}

	needed, message, err := posixRebootPending(ctx, backend, probe.PackageManager)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{NeedsChange: needed, Message: message}, nil
}

// posixRebootPending probes the distro reboot-required signals and returns
// whether a reboot is needed plus an optional status message (used when no
// signal is available). Two signals are probed, matching the spec:
//
//   - /var/run/reboot-required — the apt/unattended-upgrades convention. It is
//     checked on every distro because it is a plantable marker (used by the
//     integration suite) and because some tooling creates it on non-apt hosts.
//   - needs-restarting -r — the dnf/RHEL signal, gated to dnf systems. It exits
//     1 when a reboot is required, 0 otherwise, and 127 when the binary is
//     absent (treated as no signal available).
//
// The cached package-manager detection drives the interpretation: on apt the
// marker file is the signal (its absence means no reboot); on dnf
// needs-restarting is the signal (and the file is a bonus plantable marker);
// with no supported package manager, no signal is available.
func posixRebootPending(ctx context.Context, backend posixShellBackend, pkgManager string) (needed bool, message string, err error) {
	// 1. Marker file — checked first on every distro.
	_, _, code, err := backend.RunPOSIXCommand(ctx, "test -f /var/run/reboot-required", nil)
	if err != nil {
		return false, "", err
	}
	if code == 0 {
		return true, "", nil
	}

	// 2. dnf needs-restarting -r — the dnf/RHEL signal.
	if pkgManager == "dnf" {
		_, stderr, nrCode, err := backend.RunPOSIXCommand(ctx, "needs-restarting -r", nil)
		if err != nil {
			return false, "", err
		}
		switch nrCode {
		case 1:
			return true, "", nil
		case 0:
			return false, "", nil // signal available, says no reboot
		}
		// 127 or any other code: needs-restarting is unavailable → no signal.
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = "needs-restarting unavailable"
		}
		return false, "no reboot-required signal available; no reboot needed (" + detail + ")", nil
	}

	// 3. apt with an absent marker: the file is the signal and it says no reboot.
	if pkgManager == "apt" {
		return false, "", nil
	}

	// 4. No supported package manager: neither signal is available. Treat as
	//    no reboot needed and state so in the output.
	return false, "no reboot-required signal available; no reboot needed (no supported package manager detected)", nil
}

// applyPOSIXReboot issues `systemctl reboot` and then waits for the target to
// come back, polling a lightweight command until it reconnects within the
// timeout. The reboot command itself is expected to drop the connection; its
// error is ignored. The reconnect relies on the transport's one-shot
// reconnect-and-retry: each poll attempt re-dials on a dead connection.
//
// The real reboot+reconnect path is unit-tested against fakes only and is a
// stated limitation — it is not exercised end-to-end in CI.
func applyPOSIXReboot(ctx context.Context, backend posixShellBackend, params map[string]any) (ApplyResult, error) {
	timeout := 300
	if raw, ok := params["timeout"].(int); ok && raw > 0 {
		timeout = raw
	}
	if raw, ok := params["timeout"].(int64); ok && raw > 0 {
		timeout = int(raw)
	}
	if raw, ok := params["timeout"].(float64); ok && raw > 0 {
		timeout = int(raw)
	}

	probe, err := backend.Probe(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	if probe.Init != "systemd" {
		return ApplyResult{}, NewMissingPrerequisiteError("reboot", RuntimeKindPOSIXShell,
			"requires systemd; no init system detected on the target")
	}

	// Issue the reboot. systemctl reboot does not return until shutdown begins;
	// the connection typically drops mid-command, surfacing as a transport
	// error that is expected here.
	_, _, _, _ = backend.RunPOSIXCommand(ctx, "systemctl reboot", nil)

	if err := posixRebootReconnect(ctx, backend, timeout, time.Now, time.Sleep); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Message: "rebooted; target reconnected"}, nil
}

// posixRebootReconnect polls a lightweight command until the target answers,
// signalling it has rebooted and the transport has reconnected. now and sleep
// are injected so the loop is unit-testable against fakes without real
// sleeping. Each RunPOSIXCommand funnels through the transport's one-shot
// reconnect-and-retry, so a returned nil error means the target is back.
func posixRebootReconnect(ctx context.Context, backend posixShellBackend, timeoutSecs int, now func() time.Time, sleep func(time.Duration)) error {
	deadline := now().Add(time.Duration(timeoutSecs) * time.Second)
	for {
		_, _, _, err := backend.RunPOSIXCommand(ctx, ":", nil)
		if err == nil {
			return nil
		}
		if now().After(deadline) {
			return fmt.Errorf("reboot: target did not reconnect within %ds: %w", timeoutSecs, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sleep(5 * time.Second)
	}
}
