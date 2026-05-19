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
	CopyFile(ctx context.Context, src, dst string) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	PowerShellBinary() string
}

// newPOSIXShellRegistry builds a ModuleRegistry for remote POSIX targets (SSH-POSIX).
// LocalTarget no longer calls this function — local become uses subprocess elevation
// via newSubprocessBecomeRegistry instead.
func newPOSIXShellRegistry(backend posixShellBackend) ModuleRegistry {
	supported := ModuleRegistry{
		"directory": moduleFuncs{
			check: check(func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkPOSIXDirectory(ctx, backend, params)
			}),
			apply: applyErrOnly(func(ctx context.Context, params map[string]any) error {
				return applyPOSIXDirectory(ctx, backend, params)
			}),
		},
		"file": moduleFuncs{
			check: check(func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkPOSIXFile(ctx, backend, params)
			}),
			apply: applyErrOnly(func(ctx context.Context, params map[string]any) error {
				return applyPOSIXFile(ctx, backend, params)
			}),
		},
		"shell": moduleFuncs{
			check: check(func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkPOSIXShell(ctx, backend, params)
			}),
			apply: apply(func(ctx context.Context, params map[string]any) (string, error) {
				return applyPOSIXShell(ctx, backend, params)
			}),
		},
		"wait": moduleFuncs{
			check: check(func(ctx context.Context, params map[string]any) (bool, string, error) {
				return checkPOSIXWait(ctx, backend, params)
			}),
			apply: applyErrOnly(func(ctx context.Context, params map[string]any) error {
				return applyPOSIXWait(ctx, backend, params)
			}),
		},
	}
	if backend.PowerShellBinary() != "" {
		supported["powershell"] = moduleFuncs{
			check: checkWithOutput(func(ctx context.Context, params map[string]any, out OutputFunc) (bool, string, error) {
				return checkPowerShellModuleWithOutput(ctx, backend, params, out)
			}),
			// applyPowerShellModule streams lines through out during execution.
			// Pass nil to applyStreamed so it only extracts a single-line message
			// without re-emitting lines that were already forwarded.
			apply: func(ctx context.Context, params map[string]any, out OutputFunc) (ApplyResult, error) {
				output, err := applyPowerShellModule(ctx, backend, params, out)
				return applyStreamed(output, nil), err
			},
		}
	}
	return buildRemoteModuleRegistry(RuntimeKindPOSIXShell, supported, func(module string) error {
		switch module {
		case "powershell":
			return fmt.Errorf("posix-shell runtime: module %q requires pwsh or powershell on the remote host", module)
		case "environment", "reboot":
			return unsupportedRuntimeModuleDetailError(RuntimeKindPOSIXShell, module, "is not supported yet")
		default:
			return unsupportedRuntimeModuleDetailError(RuntimeKindPOSIXShell, module, "is Windows-only")
		}
	})
}

func checkPOSIXDirectory(ctx context.Context, backend posixShellBackend, params map[string]any) (bool, string, error) {
	path, ok := params["path"].(string)
	if !ok || path == "" {
		return false, "", fmt.Errorf("directory: required param %q is missing", "path")
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
			return false, "", err
		}
		if code != 0 {
			return false, "", fmt.Errorf("directory check exited with code %d: %s", code, strings.TrimSpace(stderr))
		}
		switch strings.TrimSpace(stdout) {
		case "missing":
			return true, "", nil
		case "dir":
			return false, "", nil
		default:
			return false, "", fmt.Errorf("directory: %q exists but is not a directory", path)
		}
	default:
		return false, "", fmt.Errorf("directory: unknown ensure value %q (want present|absent)", ensure)
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

func checkPOSIXFile(ctx context.Context, backend posixShellBackend, params map[string]any) (bool, string, error) {
	dest, ok := params["dest"].(string)
	if !ok || dest == "" {
		return false, "", fmt.Errorf("file: required param %q is missing", "dest")
	}
	ensure, _ := params["ensure"].(string)
	if ensure == "" {
		ensure = "present"
	}
	src, _ := params["src"].(string)
	content, hasContent, err := fileContentParam(params, "file", src)
	if err != nil {
		return false, "", err
	}

	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, fmt.Sprintf("if [ ! -e %q ]; then printf missing; elif [ -d %q ]; then printf dir; else printf file; fi", dest, dest), nil)
	if err != nil {
		return false, "", err
	}
	if code != 0 {
		return false, "", fmt.Errorf("file check exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	state := strings.TrimSpace(stdout)

	switch ensure {
	case "absent":
		switch state {
		case "dir":
			return false, "", fmt.Errorf("file module cannot remove directory %q: use the directory module with ensure:absent instead", dest)
		case "file":
			return true, "file exists, will remove", nil
		default: // "missing"
			return false, "", nil
		}
	case "present":
		switch state {
		case "missing":
			return true, "", nil
		case "dir":
			return false, "", fmt.Errorf("file: %q is a directory, not a file", dest)
		case "file":
			if src == "" {
				if !hasContent {
					return false, "", nil
				}
			}
			if hasContent {
				remoteHash, err := posixRemoteFileHash(ctx, backend, dest)
				if err != nil {
					return false, "", err
				}
				return hashBytes([]byte(content)) != remoteHash, "", nil
			}
			localHash, err := hashLocalFile(src)
			if err != nil {
				return false, "", err
			}
			remoteHash, err := posixRemoteFileHash(ctx, backend, dest)
			if err != nil {
				return false, "", err
			}
			return localHash != remoteHash, "", nil
		default:
			return false, "", fmt.Errorf("file: unexpected remote state %q", state)
		}
	default:
		return false, "", fmt.Errorf("file: unknown ensure value %q (want present|absent)", ensure)
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

func checkPOSIXShell(ctx context.Context, backend posixShellBackend, params map[string]any) (bool, string, error) {
	creates, _ := params["creates"].(string)
	if creates == "" {
		return true, "", nil
	}
	workingDir, _ := params["working_dir"].(string)
	if workingDir != "" {
		return posixNonZeroExitMeansChange(ctx, backend, fmt.Sprintf("cd %q && test -e %q", workingDir, creates))
	}
	return posixNonZeroExitMeansChange(ctx, backend, fmt.Sprintf("test -e %q", creates))
}

func applyPOSIXShell(ctx context.Context, backend posixShellBackend, params map[string]any) (string, error) {
	cmd, ok := params["cmd"].(string)
	if !ok || cmd == "" {
		return "", fmt.Errorf("shell: required param %q is missing", "cmd")
	}
	args, err := paramStringSlice(params, "args")
	if err != nil {
		return "", err
	}
	workingDir, _ := params["working_dir"].(string)

	var shellCmd strings.Builder
	if workingDir != "" {
		fmt.Fprintf(&shellCmd, "cd %q && ", workingDir)
	}
	shellCmd.WriteString(shellQuoteExec(cmd, args))
	stdout, stderr, code, err := backend.RunPOSIXCommand(ctx, shellCmd.String(), nil)
	if err != nil {
		return stdout, err
	}
	if code != 0 {
		return stdout, fmt.Errorf("posix command exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

func checkPOSIXWait(ctx context.Context, backend posixShellBackend, params map[string]any) (bool, string, error) {
	condition, _ := params["condition"].(string)
	targetValue, _ := params["target"].(string)
	met, err := posixWaitCondition(ctx, backend, condition, targetValue)
	if err != nil {
		return false, "", err
	}
	return !met, "", nil
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
		return false, fmt.Errorf("wait: condition %q is not supported on the posix-shell runtime", condition)
	default:
		return false, fmt.Errorf("wait: unknown condition %q (want port_open|file_exists|service_running)", condition)
	}
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

func posixNonZeroExitMeansChange(ctx context.Context, backend posixShellBackend, command string) (bool, string, error) {
	_, stderr, code, err := backend.RunPOSIXCommand(ctx, command, nil)
	if err != nil {
		return false, "", err
	}
	if code == 0 {
		return false, "", nil
	}
	if stderr != "" {
		return false, "", fmt.Errorf("check command failed: %s", strings.TrimSpace(stderr))
	}
	// A non-zero exit with no stderr is treated as "condition not met" rather
	// than an error. This is only safe for test(1) commands where exit code 1
	// unambiguously means the condition is false, not a shell or command failure.
	return true, "", nil
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
