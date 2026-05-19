package target

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func encodeBase64String(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func decodeBase64String(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

// extractIDFromStdinLine recovers the per-execution ID embedded inside the
// base64 payload of a line produced by buildPSStdinLine. Used by tests to
// know which markers to expect in the output.
//
// The payload contains both an ERR marker and a DONE marker:
//
//	Write-Output('__PFMK__<id>__ERR:'+…) … Write-Output '__PFMK__<id>__DONE'
//
// The ID is the segment between the first psMarkerBase and its first delimiter
// (__ERR: or __DONE).
func extractIDFromStdinLine(t *testing.T, line string) string {
	t.Helper()
	decoded := decodePayloadFromStdinLine(t, line)
	_, after0, ok := strings.Cut(decoded, psMarkerBase)
	if !ok {
		t.Fatalf("PFMK marker not found in decoded payload: %q", decoded)
	}
	after := after0
	// The ID ends at the first delimiter after the prefix.
	for _, delim := range []string{"__ERR:", "__DONE"} {
		if before, _, ok := strings.Cut(after, delim); ok {
			return before
		}
	}
	t.Fatalf("neither __ERR: nor __DONE found after marker prefix in: %q", decoded)
	return ""
}

// decodePayloadFromStdinLine extracts and base64-decodes the script payload
// from a line produced by buildPSStdinLine.
func decodePayloadFromStdinLine(t *testing.T, line string) string {
	t.Helper()
	const b64Start = "FromBase64String('"
	_, after, ok := strings.Cut(line, b64Start)
	if !ok {
		t.Fatalf("base64 payload not found in stdin line: %q", line)
	}
	payload, _, ok2 := strings.Cut(after, "'")
	if !ok2 {
		t.Fatalf("base64 payload not terminated in stdin line: %q", line)
	}
	decoded, err := decodeBase64String(payload)
	if err != nil {
		t.Fatalf("decode base64 payload: %v", err)
	}
	return decoded
}

// pipeBackedPS creates an sshPersistentPS whose stdin/stdout are connected to
// an in-process goroutine that simulates the PowerShell marker protocol.
//
// scriptResponses maps a substring to [output, errorMessage]. The goroutine
// reads each stdin line, matches the decoded payload against the map, and
// writes the appropriate output + marker to stdout. An empty errorMessage
// means the script succeeds.
func pipeBackedPS(t *testing.T, scriptResponses map[string][2]string) *sshPersistentPS {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	go func() {
		defer func() { _ = stdoutW.Close() }()
		sc := bufio.NewScanner(stdinR)
		for sc.Scan() {
			line := sc.Text()
			id := extractIDFromStdinLine(t, line)
			decoded := decodePayloadFromStdinLine(t, line)

			var resp [2]string
			for key, r := range scriptResponses {
				if key == "" || strings.Contains(decoded, key) {
					resp = r
					break
				}
			}

			if resp[0] != "" {
				_, _ = fmt.Fprintln(stdoutW, resp[0])
			}
			if resp[1] != "" {
				_, _ = fmt.Fprintln(stdoutW, psMarkerBase+id+"__ERR:"+encodeBase64String(resp[1]))
			} else {
				_, _ = fmt.Fprintln(stdoutW, psMarkerBase+id+"__DONE")
			}
		}
	}()

	return &sshPersistentPS{
		stdin:  stdinW,
		reader: bufio.NewReader(stdoutR),
	}
}

// ---------------------------------------------------------------------------
// buildPSStdinLine tests
// ---------------------------------------------------------------------------

func TestBuildPSStdinLine_IsSingleLine(t *testing.T) {
	line := buildPSStdinLine("Write-Output 'hello'", "testid01")
	if strings.ContainsAny(line, "\r\n") {
		t.Errorf("buildPSStdinLine produced multi-line output:\n%q", line)
	}
}

func TestBuildPSStdinLine_DifferentIDsProduceDifferentLines(t *testing.T) {
	l1 := buildPSStdinLine("Write-Output 1", "aaaaaaaa")
	l2 := buildPSStdinLine("Write-Output 1", "bbbbbbbb")
	if l1 == l2 {
		t.Error("same script with different IDs produced identical stdin lines")
	}
}

func TestBuildPSStdinLine_ContainsBase64Payload(t *testing.T) {
	line := buildPSStdinLine("Get-Item C:\\foo", "myid0001")
	if !strings.Contains(line, "FromBase64String(") {
		t.Error("stdin line does not contain a base64 payload")
	}
}

func TestBuildPSStdinLine_PayloadContainsUserScript(t *testing.T) {
	userScript := "Get-Item 'C:\\Program Files'"
	line := buildPSStdinLine(userScript, "myid0001")
	decoded := decodePayloadFromStdinLine(t, line)
	if !strings.Contains(decoded, userScript) {
		t.Errorf("decoded payload does not contain user script\ndecoded: %q\nscript:  %q", decoded, userScript)
	}
}

func TestBuildPSStdinLine_Exit0ReplacedWithReturn(t *testing.T) {
	script := "Write-Output 'hello'\nexit 0\nWrite-Output 'never'"
	line := buildPSStdinLine(script, "myid0003")
	decoded := decodePayloadFromStdinLine(t, line)
	if strings.Contains(decoded, "exit 0") {
		t.Error("decoded payload still contains 'exit 0'; expected it to be replaced with 'return'")
	}
	if !strings.Contains(decoded, "return") {
		t.Error("decoded payload does not contain 'return' after exit 0 replacement")
	}
}

func TestBuildPSStdinLine_PayloadContainsDoneMark(t *testing.T) {
	id := "myid0002"
	line := buildPSStdinLine("Write-Output 1", id)
	decoded := decodePayloadFromStdinLine(t, line)
	want := psMarkerBase + id + "__DONE"
	if !strings.Contains(decoded, want) {
		t.Errorf("decoded payload does not contain DONE marker %q\ndecoded: %q", want, decoded)
	}
}

func TestBuildPSStdinLine_ResetsPersistentPreferences(t *testing.T) {
	line := buildPSStdinLine("Write-Output 'hello'", "myid0004")
	decoded := decodePayloadFromStdinLine(t, line)
	if !strings.Contains(decoded, "$ErrorActionPreference='Continue'") {
		t.Errorf("decoded payload does not reset ErrorActionPreference:\n%s", decoded)
	}
}

// ---------------------------------------------------------------------------
// readPSOutput tests
// ---------------------------------------------------------------------------

func TestReadPSOutput_SuccessNoOutput(t *testing.T) {
	id := "abc12345"
	raw := psMarkerBase + id + "__DONE\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestReadPSOutput_SuccessWithSingleLine(t *testing.T) {
	id := "abc12345"
	raw := "hello world\n" + psMarkerBase + id + "__DONE\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello world" {
		t.Errorf("got %q, want %q", out, "hello world")
	}
}

func TestReadPSOutput_SuccessWithMultipleLines(t *testing.T) {
	id := "abc12345"
	raw := "line one\nline two\nline three\n" + psMarkerBase + id + "__DONE\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "line one\nline two\nline three"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestReadPSOutput_ScriptError(t *testing.T) {
	id := "err12345"
	errMsg := "something went wrong in PS"
	raw := psMarkerBase + id + "__ERR:" + encodeBase64String(errMsg) + "\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err == nil {
		t.Fatalf("expected error, got output %q", out)
	}
	if !strings.Contains(err.Error(), errMsg) {
		t.Errorf("error %q does not contain %q", err.Error(), errMsg)
	}
	if isSessionError(err) {
		t.Error("script error was misclassified as a session error")
	}
}

func TestReadPSOutput_ScriptErrorPreservesOutputBefore(t *testing.T) {
	id := "err12345"
	errMsg := "kaboom"
	raw := "partial output\n" + psMarkerBase + id + "__ERR:" + encodeBase64String(errMsg) + "\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err == nil {
		t.Fatalf("expected error, got output %q", out)
	}
	if out != "partial output" {
		t.Errorf("expected partial output before error, got %q", out)
	}
}

func TestReadPSOutput_SessionClosedBeforeMarker(t *testing.T) {
	id := "sess1234"
	// EOF without marker — simulates unexpected session close.

	_, err := readPSOutput(bufio.NewReader(strings.NewReader("some output\n")), id, nil)
	if err == nil {
		t.Fatal("expected error when session closes without marker")
	}
	if !isSessionError(err) {
		t.Errorf("expected *psSessionError, got %T: %v", err, err)
	}
}

func TestReadPSOutput_EmptyReader(t *testing.T) {
	_, err := readPSOutput(bufio.NewReader(strings.NewReader("")), "anyid00", nil)
	if err == nil {
		t.Fatal("expected error on empty reader")
	}
	if !isSessionError(err) {
		t.Errorf("expected *psSessionError on empty reader, got %T: %v", err, err)
	}
}

func TestReadPSOutput_WrongIDTreatedAsOutput(t *testing.T) {
	// A marker for a different session ID must be collected as plain output
	// until the correct marker arrives.
	id := "myid1234"
	otherId := "otherid9"
	raw := psMarkerBase + otherId + "__DONE\n" + // wrong id — should be plain output
		"real output\n" +
		psMarkerBase + id + "__DONE\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "real output") {
		t.Errorf("expected 'real output' in %q", out)
	}
	// The wrong-id marker line should also appear in the collected output.
	if !strings.Contains(out, psMarkerBase+otherId+"__DONE") {
		t.Errorf("wrong-id marker line not present in collected output: %q", out)
	}
}

func TestReadPSOutput_AllowsLongSingleLine(t *testing.T) {
	id := "longline"
	longLine := strings.Repeat("x", 2<<20)
	raw := longLine + "\n" + psMarkerBase + id + "__DONE\n"

	out, err := readPSOutput(bufio.NewReader(strings.NewReader(raw)), id, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != longLine {
		t.Fatalf("long output line was not preserved: got %d bytes, want %d", len(out), len(longLine))
	}
}

// ---------------------------------------------------------------------------
// generateSessionID / isSessionError tests
// ---------------------------------------------------------------------------

func TestGenerateSessionID_Format(t *testing.T) {
	id := generateSessionID()
	if len(id) != 16 {
		t.Errorf("expected 16-char hex ID, got %q (len %d)", id, len(id))
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("non-hex character %q in ID %q", c, id)
		}
	}
}

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 200)
	for range 200 {
		id := generateSessionID()
		if seen[id] {
			t.Fatalf("collision: %q generated twice in 200 attempts", id)
		}
		seen[id] = true
	}
}

func TestIsSessionError_Discrimination(t *testing.T) {
	sessionErr := &psSessionError{cause: fmt.Errorf("transport closed")}
	if !isSessionError(sessionErr) {
		t.Error("isSessionError returned false for *psSessionError")
	}
	plainErr := fmt.Errorf("script failed: exit code 1")
	if isSessionError(plainErr) {
		t.Error("isSessionError returned true for plain error")
	}
}

// ---------------------------------------------------------------------------
// WinRMTarget — persistent session
// ---------------------------------------------------------------------------

func TestWinRMTarget_UsesLegacyWhenClientHasNoCreateShell(t *testing.T) {
	// fakeWinRMClient does NOT implement winRMShellCreator.
	var legacyCalled bool
	tgt := NewWinRMTarget(WinRMConfig{Host: "host", Username: "u", Password: "p"})
	tgt.client = &fakeWinRMClient{
		runPS: func(_ context.Context, _ string) (string, string, int, error) {
			legacyCalled = true
			return "ok", "", 0, nil
		},
	}

	out, err := tgt.runPS(context.Background(), `Write-Output 'hi'`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Errorf("expected legacy output 'ok', got %q", out)
	}
	if !legacyCalled {
		t.Error("legacy RunPSWithContext was not called")
	}
}

func TestWinRMTarget_ResetPSSessionClearsSession(t *testing.T) {
	tgt := NewWinRMTarget(WinRMConfig{Host: "host"})
	// Manually set a non-nil session with a no-op close.
	tgt.psSession = &winRMPersistentPS{}
	tgt.resetPSSession()
	if tgt.psSession != nil {
		t.Error("psSession should be nil after resetPSSession")
	}
}

// ---------------------------------------------------------------------------
// sshWindowsPowerShellRuntime — persistent session integration
// ---------------------------------------------------------------------------

func TestSSHWindowsRuntime_PersistentSession_Success(t *testing.T) {
	ps := pipeBackedPS(t, map[string][2]string{
		"Write-Output": {"hello from PS", ""},
	})

	rt := &sshWindowsPowerShellRuntime{
		target: &SSHTarget{
			runner: &fakeSSHRunner{
				run: func(_ context.Context, _ string, _ []byte) (string, string, int, error) {
					t.Error("legacy Run should not be called when persistent session is active")
					return "", "", 0, nil
				},
			},
		},
		binary:    "powershell.exe",
		psSession: ps,
	}

	out, err := rt.RunPowerShellScript(context.Background(), "Write-Output 'hello'", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello from PS" {
		t.Errorf("got %q, want %q", out, "hello from PS")
	}
}

func TestSSHWindowsRuntime_PersistentSession_ScriptError(t *testing.T) {
	ps := pipeBackedPS(t, map[string][2]string{
		"": {"", "script threw an exception"},
	})

	rt := &sshWindowsPowerShellRuntime{
		target: &SSHTarget{
			runner: &fakeSSHRunner{
				run: func(_ context.Context, _ string, _ []byte) (string, string, int, error) {
					t.Error("legacy Run should not be called for script-level errors")
					return "", "", 0, nil
				},
			},
		},
		binary:    "powershell.exe",
		psSession: ps,
	}

	_, err := rt.RunPowerShellScript(context.Background(), "throw 'oops'", nil)
	if err == nil {
		t.Fatal("expected error from script, got nil")
	}
	if isSessionError(err) {
		t.Errorf("script error was misclassified as session error: %v", err)
	}
	if !strings.Contains(err.Error(), "script threw an exception") {
		t.Errorf("error %q does not contain expected message", err.Error())
	}
}

func TestSSHWindowsRuntime_FallsBackToLegacyOnSessionError(t *testing.T) {
	// stdin pipe is immediately closed so any write will fail.
	stdinR, stdinW := io.Pipe()
	_ = stdinW.Close()
	_ = stdinR.Close()

	ps := &sshPersistentPS{
		stdin:  stdinW,
		reader: bufio.NewReader(strings.NewReader("")),
	}

	var legacyCalled bool
	rt := &sshWindowsPowerShellRuntime{
		target: &SSHTarget{
			runner: &fakeSSHRunner{
				run: func(_ context.Context, command string, _ []byte) (string, string, int, error) {
					legacyCalled = true
					return "legacy output", "", 0, nil
				},
			},
		},
		binary:    "powershell.exe",
		psSession: ps,
	}

	out, err := rt.RunPowerShellScript(context.Background(), "Write-Output 'test'", nil)
	if err != nil {
		t.Fatalf("unexpected error after legacy fallback: %v", err)
	}
	if !legacyCalled {
		t.Error("legacy path was not called after session transport error")
	}
	if out != "legacy output" {
		t.Errorf("got %q, want 'legacy output'", out)
	}

	// Session must be reset so the next call can create a fresh one.
	rt.psSessionMu.Lock()
	sessionNil := rt.psSession == nil
	rt.psSessionMu.Unlock()
	if !sessionNil {
		t.Error("psSession should be nil after session error + fallback")
	}
}

func TestSSHWindowsRuntime_FallsBackToLegacyWhenNoSessionCreator(t *testing.T) {
	// fakeSSHRunner does NOT implement sshSessionCreator, so getOrCreatePSSession
	// must return nil and fall through to the legacy path.
	var legacyCalled bool
	tgt := &SSHTarget{
		runner: &fakeSSHRunner{
			run: func(_ context.Context, _ string, _ []byte) (string, string, int, error) {
				legacyCalled = true
				return "legacy ok", "", 0, nil
			},
		},
	}

	rt := &sshWindowsPowerShellRuntime{target: tgt, binary: "powershell.exe"}
	tgt.runtimeMu.Lock()
	tgt.runtime = rt
	tgt.runtimeMu.Unlock()

	out, err := rt.RunPowerShellScript(context.Background(), "Write-Output 'test'", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !legacyCalled {
		t.Error("legacy run was not called when no session creator is available")
	}
	if out != "legacy ok" {
		t.Errorf("got %q, want 'legacy ok'", out)
	}
}

func TestSSHWindowsRuntime_MultipleScriptsReuseSession(t *testing.T) {
	ps := pipeBackedPS(t, map[string][2]string{
		"": {"result", ""},
	})

	var legacyCalls int
	rt := &sshWindowsPowerShellRuntime{
		target: &SSHTarget{
			runner: &fakeSSHRunner{
				run: func(_ context.Context, _ string, _ []byte) (string, string, int, error) {
					legacyCalls++
					return "", "", 0, nil
				},
			},
		},
		binary:    "powershell.exe",
		psSession: ps,
	}

	for i := range 5 {
		if _, err := rt.RunPowerShellScript(context.Background(), "Write-Output 'x'", nil); err != nil {
			t.Fatalf("script %d failed: %v", i, err)
		}
	}
	if legacyCalls > 0 {
		t.Errorf("legacy run was called %d time(s); expected 0 (persistent session in use)", legacyCalls)
	}
}

func TestSSHWindowsRuntime_SessionRemainsAliveAfterScriptError(t *testing.T) {
	// First call returns an error; second call should succeed — both through
	// the same persistent session (session-level errors cause reset; script
	// errors do not).
	callNum := 0
	ps := pipeBackedPS(t, map[string][2]string{
		"first":  {"", "first call error"},
		"second": {"second output", ""},
	})

	rt := &sshWindowsPowerShellRuntime{
		target:    &SSHTarget{runner: &fakeSSHRunner{}},
		binary:    "powershell.exe",
		psSession: ps,
	}

	callNum++
	_, err := rt.RunPowerShellScript(context.Background(), "first script", nil)
	if err == nil {
		t.Fatalf("call %d: expected error, got nil", callNum)
	}
	if isSessionError(err) {
		t.Fatalf("call %d: script error misclassified as session error", callNum)
	}

	// Session must still be alive after a non-fatal script error.
	rt.psSessionMu.Lock()
	sessionStillAlive := rt.psSession != nil
	rt.psSessionMu.Unlock()
	if !sessionStillAlive {
		t.Fatal("session was reset after a script error; it should only reset on transport errors")
	}

	callNum++
	out, err := rt.RunPowerShellScript(context.Background(), "second script", nil)
	if err != nil {
		t.Fatalf("call %d: unexpected error: %v", callNum, err)
	}
	if out != "second output" {
		t.Errorf("call %d: got %q, want 'second output'", callNum, out)
	}
}
