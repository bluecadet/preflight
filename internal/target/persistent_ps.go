package target

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// psSessionRunner is the run side of a persistent PowerShell session, regardless
// of transport. Both WinRM and SSH Windows-PS sessions implement it through the
// same stdin/stdout marker protocol (buildPSStdinLine + readPSOutput); only
// session setup and teardown differ between transports.
type psSessionRunner interface {
	run(ctx context.Context, script string, out OutputFunc) (string, error)
}

// runPSWithFallback executes script through the persistent PS session when one
// is available, falling back to legacy per-invocation execution on session
// errors. The acquireSession callback returns (nil, nil) when no session can be
// created (e.g. test fakes that don't implement the creator interface). On a
// *psSessionError the session is reset (the underlying error is passed to the
// callback for logging) before falling back; on a script-level error the
// result is returned as-is without falling back.
//
// This pattern was previously copy-pasted between WinRMTarget.runPS and
// sshWindowsPowerShellRuntime.RunPowerShellScript.
func runPSWithFallback(
	ctx context.Context,
	script string,
	out OutputFunc,
	acquireSession func(context.Context) (psSessionRunner, error),
	resetSession func(cause error),
	legacy func(context.Context, string, OutputFunc) (string, error),
) (string, error) {
	ps, err := acquireSession(ctx)
	if err == nil && ps != nil {
		result, psErr := ps.run(ctx, script, out)
		if psErr == nil {
			return result, nil
		}
		if isSessionError(psErr) {
			resetSession(psErr)
		} else {
			return result, psErr
		}
	}
	return legacy(ctx, script, out)
}

// psMarkerBase is the unique prefix used to delimit script output from control
// lines in a persistent PowerShell stdin/stdout session. The marker is combined
// with a per-execution random ID so accidental collisions with user script output
// are astronomically unlikely.
const psMarkerBase = "__PFMK__"

// psSessionError signals that the underlying transport for a persistent PS
// session failed (e.g. the pipe closed, the SSH channel dropped). The caller
// should discard the session and fall back to per-command execution.
type psSessionError struct{ cause error }

func (e *psSessionError) Error() string { return "ps session: " + e.cause.Error() }
func (e *psSessionError) Unwrap() error { return e.cause }

func isSessionError(err error) bool {
	var se *psSessionError
	return errors.As(err, &se)
}

// generateSessionID returns a random 16-character hex string used to make
// per-execution markers unique.
func generateSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// buildPSStdinLine returns a single line that, when written to a persistent
// PowerShell stdin session (started with -Command -), executes script and then
// emits one of two terminator lines:
//
//	__PFMK__<id>__DONE              – script completed without a terminating error
//	__PFMK__<id>__ERR:<b64>        – script threw; b64 is base64(UTF-8 error message)
//
// The user script itself is embedded inside a helper function defined in the
// base64-encoded payload. Wrapping in a function means any "exit 0" that gets
// rewritten to "return" exits the function scope rather than the PowerShell
// host process, so the persistent session stays alive.
func buildPSStdinLine(script, id string) string {
	doneMark := psMarkerBase + id + "__DONE"
	errMark := psMarkerBase + id + "__ERR:"

	// Replace `exit 0` with `return` so that early-exit paths in module
	// scripts return from __pf_run instead of killing the powershell.exe host.
	// Non-zero exits are left alone: they terminate the session, which is the
	// right behaviour for unexpected error conditions.
	script = strings.ReplaceAll(script, "exit 0", "return")

	// Wrapper: define the script as a function so `return` exits only that
	// function, then call it inside a try/catch that emits the completion
	// marker regardless of outcome.
	wrapped := `$ErrorActionPreference='Continue'
$ProgressPreference='SilentlyContinue'
$__pf_err=$null
function __pf_run{
` + script + `
}
try{
__pf_run
}catch{
$__pf_err=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($_.ToString()))
}
if($__pf_err-ne$null){Write-Output('` + errMark + `'+$__pf_err)}else{Write-Output '` + doneMark + `'}
[Console]::Out.Flush()`

	encoded := base64.StdEncoding.EncodeToString([]byte(wrapped))
	// A single-line expression: decode the script into a string, compile it into
	// a ScriptBlock, and invoke it in a child scope with &.
	return `&([ScriptBlock]::Create([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + encoded + `'))))`
}

// readPSOutput reads lines from reader until it sees the completion marker for
// id. Lines before the marker are the user script's stdout. Returns the output
// and any script-level error. Returns *psSessionError if the underlying reader
// closes before the marker is found.
//
// When out is non-nil, each user-visible output line is forwarded to it as it
// arrives, enabling real-time streaming for persistent-session callers.
func readPSOutput(reader *bufio.Reader, id string, out OutputFunc) (string, error) {
	doneMark := psMarkerBase + id + "__DONE"
	errMark := psMarkerBase + id + "__ERR:"

	var lines []string
	for {
		line, readErr := readPowerShellOutputLine(reader)
		if readErr != nil && line == "" {
			return "", &psSessionError{cause: readErr}
		}
		switch {
		case line == doneMark:
			return strings.Join(lines, "\n"), nil
		case strings.HasPrefix(line, errMark):
			b64 := line[len(errMark):]
			decoded, decErr := base64.StdEncoding.DecodeString(b64)
			if decErr != nil {
				return strings.Join(lines, "\n"), fmt.Errorf("ps error (decode failed): %s", b64)
			}
			return strings.Join(lines, "\n"), errors.New(string(decoded))
		default:
			lines = append(lines, line)
			if out != nil {
				out(line)
			}
		}
		if readErr != nil {
			return "", &psSessionError{cause: readErr}
		}
	}
}

func readPowerShellOutputLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line == "" {
			return "", errors.New("EOF")
		}
		if line == "" {
			return "", err
		}
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, err
}
