package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// buildTestBinary compiles the preflight binary for testing the __module-exec subcommand.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	binPath := dir + "/preflight-test"
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\noutput: %s", err, out)
	}
	return binPath
}

func runModuleExecRPC(t *testing.T, binPath, moduleName, reqJSON string) []string {
	t.Helper()
	cmd := exec.Command(binPath, "__module-exec", moduleName)
	cmd.Stdin = strings.NewReader(reqJSON + "\n")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestModuleExec_UnknownModule(t *testing.T) {
	binPath := buildTestBinary(t)
	lines := runModuleExecRPC(t, binPath, "no-such-module", `{"jsonrpc":"2.0","id":1,"method":"check","params":{"args":{}}}`)
	if len(lines) == 0 {
		t.Fatal("expected at least one output line")
	}
	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw: %q)", err, lines[0])
	}
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
}

func TestModuleExec_DirectoryCheck(t *testing.T) {
	if os.Getenv("CI") == "" && os.Getenv("RUN_MODULE_EXEC_TESTS") == "" {
		t.Skip("skipping binary round-trip test (set RUN_MODULE_EXEC_TESTS=1 to run)")
	}
	binPath := buildTestBinary(t)
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	lines := runModuleExecRPC(t, binPath, "directory", req)
	if len(lines) == 0 {
		t.Fatal("expected at least one output line")
	}
	var resp struct {
		Result *struct {
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Result == nil || resp.Result.Name != "directory" {
		t.Errorf("expected name=directory, got: %v", resp.Result)
	}
}

func TestModuleExec_NotInHelp(t *testing.T) {
	binPath := buildTestBinary(t)
	cmd := exec.Command(binPath, "--help")
	out, _ := cmd.Output()
	if strings.Contains(string(out), "__module-exec") {
		t.Errorf("__module-exec should be hidden but appears in --help output")
	}
}
