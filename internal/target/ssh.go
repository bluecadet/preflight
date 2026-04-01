package target

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/bluecadet/preflight/internal/tasklog"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	// KnownHostsFile is the path to a known_hosts file used to verify the
	// remote host key. When empty the connection proceeds without host key
	// verification (insecure; only acceptable on isolated networks).
	KnownHostsFile string
	// HostKeyAlgorithms restricts the accepted host key algorithms when
	// KnownHostsFile is set. If nil, all algorithms supported by the
	// known_hosts file are accepted.
	HostKeyAlgorithms []string
}

type sshRunner interface {
	Run(ctx context.Context, command string, stdin []byte) (stdout string, stderr string, exitCode int, err error)
}

type sshRunnerFactory func(SSHConfig) (sshRunner, error)

var defaultSSHRunnerFactory sshRunnerFactory = func(cfg SSHConfig) (sshRunner, error) {
	authMethods := make([]ssh.AuthMethod, 0, 2)
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			if data, readErr := os.ReadFile(cfg.PrivateKey); readErr == nil {
				signer, err = ssh.ParsePrivateKey(data)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("ssh: parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	hostKeyCallback := ssh.InsecureIgnoreHostKey() //nolint:gosec // insecure fallback when KnownHostsFile is not configured
	if cfg.KnownHostsFile != "" {
		cb, err := knownhosts.New(cfg.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("ssh: load known_hosts %q: %w", cfg.KnownHostsFile, err)
		}
		hostKeyCallback = cb
	}
	clientConfig := &ssh.ClientConfig{
		User:              cfg.Username,
		Auth:              authMethods,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: cfg.HostKeyAlgorithms,
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		return nil, err
	}
	return &sshClientRunner{client: client}, nil
}

// SSHTarget communicates with a remote machine over SSH.
type SSHTarget struct {
	config        SSHConfig
	runnerFactory sshRunnerFactory
	mu            sync.Mutex
	runner        sshRunner
}

func NewSSHTarget(cfg SSHConfig) *SSHTarget {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	return &SSHTarget{
		config:        cfg,
		runnerFactory: defaultSSHRunnerFactory,
	}
}

func (t *SSHTarget) Execute(ctx context.Context, taskID string, module string, params map[string]any, dryRun bool) (Result, error) {
	needsChange, err := t.checkModule(ctx, module, params)
	if err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	if !needsChange {
		return Result{TaskID: taskID, Status: StatusOK, Message: "already in desired state"}, nil
	}
	if dryRun {
		return Result{TaskID: taskID, Status: StatusChanged, Message: "would apply change (dry-run)"}, nil
	}
	if err := t.applyModule(ctx, module, params); err != nil {
		return Result{TaskID: taskID, Status: StatusFailed, Error: err}, err
	}
	return Result{TaskID: taskID, Status: StatusChanged, Message: "change applied"}, nil
}

func (t *SSHTarget) CopyFile(ctx context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf("mkdir -p %q && base64 -d > %q", shellDir(dst), dst)
	_, _, code, err := t.run(ctx, cmd, []byte(encoded))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh copy exited with code %d", code)
	}
	return nil
}

func (t *SSHTarget) ReadFile(ctx context.Context, path string) ([]byte, error) {
	stdout, _, code, err := t.run(ctx, fmt.Sprintf("base64 < %q", path), nil)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("ssh read exited with code %d", code)
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(stdout))
}

func (t *SSHTarget) Reachable(ctx context.Context) (bool, error) {
	_, _, code, err := t.run(ctx, "echo preflight", nil)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

func (t *SSHTarget) Info(ctx context.Context) (TargetInfo, error) {
	stdout, _, code, err := t.run(ctx, "printf '%s|%s|%s\\n' \"$(hostname)\" \"$(uname -s)\" \"$(uname -m)\"", nil)
	if err != nil {
		return TargetInfo{}, err
	}
	if code != 0 {
		return TargetInfo{}, fmt.Errorf("ssh info exited with code %d", code)
	}
	parts := strings.Split(strings.TrimSpace(stdout), "|")
	if len(parts) != 3 {
		return TargetInfo{}, fmt.Errorf("ssh info: unexpected output %q", stdout)
	}
	return TargetInfo{
		Hostname:  parts[0],
		OSVersion: parts[1],
		Arch:      parts[2],
	}, nil
}

func (t *SSHTarget) clientRunner() (sshRunner, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.runner != nil {
		return t.runner, nil
	}
	if t.runnerFactory == nil {
		t.runnerFactory = defaultSSHRunnerFactory
	}
	runner, err := t.runnerFactory(t.config)
	if err != nil {
		return nil, err
	}
	t.runner = runner
	return runner, nil
}

func (t *SSHTarget) run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	runner, err := t.clientRunner()
	if err != nil {
		return "", "", 0, err
	}
	stdout, stderr, code, err := runner.Run(ctx, command, stdin)
	tasklog.EmitLines(ctx, "stdout", stdout)
	tasklog.EmitLines(ctx, "stderr", stderr)
	return stdout, stderr, code, err
}

func (t *SSHTarget) checkModule(ctx context.Context, module string, params map[string]any) (bool, error) {
	switch module {
	case "directory":
		path, _ := params["path"].(string)
		ensure, _ := params["ensure"].(string)
		if ensure == "absent" {
			return t.nonZeroExitMeansChange(ctx, fmt.Sprintf("test ! -e %q", path))
		}
		return t.nonZeroExitMeansChange(ctx, fmt.Sprintf("test -d %q", path))
	case "file":
		dest, _ := params["dest"].(string)
		ensure, _ := params["ensure"].(string)
		if ensure == "absent" {
			return t.nonZeroExitMeansChange(ctx, fmt.Sprintf("test ! -e %q", dest))
		}
		return t.nonZeroExitMeansChange(ctx, fmt.Sprintf("test -f %q", dest))
	case "shell":
		creates, _ := params["creates"].(string)
		if creates == "" {
			return true, nil
		}
		return t.nonZeroExitMeansChange(ctx, fmt.Sprintf("test -e %q", creates))
	default:
		return false, fmt.Errorf("ssh: module %q is not supported yet", module)
	}
}

func (t *SSHTarget) applyModule(ctx context.Context, module string, params map[string]any) error {
	switch module {
	case "directory":
		path, _ := params["path"].(string)
		ensure, _ := params["ensure"].(string)
		if ensure == "absent" {
			return t.mustRun(ctx, fmt.Sprintf("rm -rf %q", path))
		}
		return t.mustRun(ctx, fmt.Sprintf("mkdir -p %q", path))
	case "file":
		dest, _ := params["dest"].(string)
		ensure, _ := params["ensure"].(string)
		if ensure == "absent" {
			return t.mustRun(ctx, fmt.Sprintf("rm -f %q", dest))
		}
		if src, _ := params["src"].(string); src != "" {
			return t.CopyFile(ctx, src, dest)
		}
		return t.mustRun(ctx, fmt.Sprintf("mkdir -p %q && : > %q", shellDir(dest), dest))
	case "shell":
		cmd, _ := params["cmd"].(string)
		args, err := sshStringSlice(params["args"])
		if err != nil {
			return err
		}
		workingDir, _ := params["working_dir"].(string)
		var shellCmd strings.Builder
		if workingDir != "" {
			shellCmd.WriteString(fmt.Sprintf("cd %q && ", workingDir))
		}
		shellCmd.WriteString(shellQuoteExec(cmd, args))
		return t.mustRun(ctx, shellCmd.String())
	default:
		return fmt.Errorf("ssh: module %q is not supported yet", module)
	}
}

func (t *SSHTarget) nonZeroExitMeansChange(ctx context.Context, command string) (bool, error) {
	_, stderr, code, err := t.run(ctx, command, nil)
	if err != nil {
		return false, err
	}
	if code == 0 {
		return false, nil
	}
	if stderr != "" {
		return true, nil
	}
	return true, nil
}

func (t *SSHTarget) mustRun(ctx context.Context, command string) error {
	_, stderr, code, err := t.run(ctx, command, nil)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("ssh command exited with code %d: %s", code, strings.TrimSpace(stderr))
	}
	return nil
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

func sshStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return typed, nil
	case []any:
		result := make([]string, 0, len(typed))
		for i, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("args[%d] must be string, got %T", i, item)
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("args must be []string, got %T", value)
	}
}

type sshClientRunner struct {
	client *ssh.Client
}

func (r *sshClientRunner) Run(ctx context.Context, command string, stdin []byte) (string, string, int, error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", "", 0, err
	}
	defer session.Close()

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
