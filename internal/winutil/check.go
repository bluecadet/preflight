package winutil

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// PowerShellCheckResult is the normalized result emitted by the wrapped
// powershell check_script contract.
type PowerShellCheckResult struct {
	NeedsChange bool   `json:"needs_change"`
	Message     string `json:"message,omitempty"`
}

// JSONVarScript emits a PowerShell snippet that reconstructs a Go value as a
// PowerShell variable through JSON + base64 encoding.
func JSONVarScript(name string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", name, err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf(
		"$%s = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('%s')) | ConvertFrom-Json",
		name,
		encoded,
	), nil
}

// BuildPowerShellCheckScript wraps a user-authored check script and normalizes
// its return value into a compact JSON object for Go-side parsing.
func BuildPowerShellCheckScript(checkScript string) (string, error) {
	varScript, err := JSONVarScript("checkScript", checkScript)
	if err != nil {
		return "", err
	}

	return varScript + `
$ErrorActionPreference = 'Stop'
$block = [ScriptBlock]::Create($checkScript)
$result = & $block
if ($null -eq $result) {
  throw "powershell check_script must return a bool or object"
}
if ($result -is [System.Array]) {
  if ($result.Count -ne 1) {
    throw "powershell check_script returned multiple pipeline values"
  }
  $result = $result[0]
}
if ($result -is [bool]) {
  [pscustomobject]@{
    needs_change = [bool]$result
    message = $null
  } | ConvertTo-Json -Compress
  exit 0
}
$needsProp = $result.PSObject.Properties['needs_change']
if ($null -eq $needsProp) {
  throw "powershell check_script must return a bool or object with needs_change"
}
$messageProp = $result.PSObject.Properties['message']
[pscustomobject]@{
  needs_change = [bool]$needsProp.Value
  message = if ($null -ne $messageProp) { [string]$messageProp.Value } else { $null }
} | ConvertTo-Json -Compress
`, nil
}

// ParsePowerShellCheckResult decodes the normalized JSON emitted by
// BuildPowerShellCheckScript.
func ParsePowerShellCheckResult(out []byte) (PowerShellCheckResult, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return PowerShellCheckResult{}, fmt.Errorf("empty powershell check result")
	}

	var raw map[string]any
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return PowerShellCheckResult{}, fmt.Errorf("parse powershell check result: %w", err)
	}

	needsRaw, ok := raw["needs_change"]
	if !ok {
		return PowerShellCheckResult{}, fmt.Errorf("powershell check result missing needs_change")
	}
	needs, err := asBool(needsRaw)
	if err != nil {
		return PowerShellCheckResult{}, fmt.Errorf("powershell check result needs_change: %w", err)
	}

	result := PowerShellCheckResult{NeedsChange: needs}
	if message, ok := raw["message"]; ok && message != nil {
		result.Message = fmt.Sprintf("%v", message)
	}
	return result, nil
}

func asBool(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected bool, got %T", value)
}
