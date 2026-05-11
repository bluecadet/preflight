package winutil

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// PowerShellCheckResult is the normalized result emitted by the wrapped
// powershell check_script contract.
type PowerShellCheckResult struct {
	NeedsChange bool   `json:"needs_change"`
	Message     string `json:"message,omitempty"`
}

const powerShellCheckResultPrefix = "__PREFLIGHT_CHECK_RESULT__:"

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
function Format-PreflightCheckValue($value) {
  if ($null -eq $value) {
    return '<null>'
  }
  $text = try { [string]$value } catch { "<$($value.GetType().FullName)>" }
  $text = ($text -replace "` + "`r" + `", ' ' -replace "` + "`n" + `", ' ').Trim()
  if ($text.Length -gt 200) {
    $text = $text.Substring(0, 200) + '...'
  }
  if ([string]::IsNullOrWhiteSpace($text)) {
    return "<$($value.GetType().FullName)>"
  }
  return $text
}
$block = [ScriptBlock]::Create($checkScript)
$values = @(& $block)
if ($values.Count -eq 0) {
  throw "powershell check_script produced no result; return a bool or an object with needs_change as the final output"
}
$result = $values[$values.Count - 1]
if ($values.Count -gt 1) {
  foreach ($entry in $values[0..($values.Count - 2)]) {
    Write-Output $entry
  }
}
$payload = $null
if ($result -is [bool]) {
  $payload = [pscustomobject]@{
    needs_change = [bool]$result
    message = $null
  }
} elseif ($result -is [System.Collections.IDictionary]) {
  if (-not $result.Contains('needs_change')) {
    $typeName = $result.GetType().FullName
    $preview = Format-PreflightCheckValue $result
    throw "powershell check_script must return a bool or object with needs_change as its final output; last output was $($typeName): $($preview). Suppress command output or assign it to a variable if it is not the check result."
  }
  $payload = [pscustomobject]@{
    needs_change = [bool]$result['needs_change']
    message = if ($result.Contains('message')) { [string]$result['message'] } else { $null }
  }
} else {
  $needsProp = $result.PSObject.Properties['needs_change']
  if ($null -eq $needsProp) {
    $typeName = if ($null -eq $result) { '<null>' } else { $result.GetType().FullName }
    $preview = Format-PreflightCheckValue $result
    throw "powershell check_script must return a bool or object with needs_change as its final output; last output was $($typeName): $($preview). Suppress command output or assign it to a variable if it is not the check result."
  }
  $messageProp = $result.PSObject.Properties['message']
  $payload = [pscustomobject]@{
    needs_change = [bool]$needsProp.Value
    message = if ($null -ne $messageProp) { [string]$messageProp.Value } else { $null }
  }
}
$json = $payload | ConvertTo-Json -Compress
$bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
Write-Output ('` + powerShellCheckResultPrefix + `' + [System.Convert]::ToBase64String($bytes))
`, nil
}

// ParsePowerShellCheckResult decodes the normalized JSON emitted by
// BuildPowerShellCheckScript.
func ParsePowerShellCheckResult(out []byte) (PowerShellCheckResult, error) {
	result, _, err := ParsePowerShellCheckOutput(out)
	return result, err
}

func ParsePowerShellCheckOutput(out []byte) (PowerShellCheckResult, []string, error) {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return PowerShellCheckResult{}, nil, fmt.Errorf("empty powershell check result")
	}

	lines := splitOutputLines(string(trimmed))
	if idx := slices.IndexFunc(lines, IsPowerShellCheckResultLine); idx >= 0 {
		outputLines := make([]string, 0, len(lines)-1)
		for i, line := range lines {
			if i == idx {
				continue
			}
			outputLines = append(outputLines, line)
		}
		payload, err := parsePowerShellCheckResultPayload(lines[idx])
		if err != nil {
			return PowerShellCheckResult{}, nil, err
		}
		return payload, outputLines, nil
	}

	return parsePowerShellCheckResultJSON(trimmed)
}

func IsPowerShellCheckResultLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), powerShellCheckResultPrefix)
}

func parsePowerShellCheckResultPayload(line string) (PowerShellCheckResult, error) {
	encoded := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), powerShellCheckResultPrefix))
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return PowerShellCheckResult{}, fmt.Errorf("decode powershell check result: %w", err)
	}
	result, _, err := parsePowerShellCheckResultJSON(data)
	return result, err
}

func parsePowerShellCheckResultJSON(payload []byte) (PowerShellCheckResult, []string, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return PowerShellCheckResult{}, nil, fmt.Errorf("parse powershell check result: %w", err)
	}

	needsRaw, ok := raw["needs_change"]
	if !ok {
		return PowerShellCheckResult{}, nil, fmt.Errorf("powershell check result missing needs_change")
	}
	needs, err := asBool(needsRaw)
	if err != nil {
		return PowerShellCheckResult{}, nil, fmt.Errorf("powershell check result needs_change: %w", err)
	}

	result := PowerShellCheckResult{NeedsChange: needs}
	if message, ok := raw["message"]; ok && message != nil {
		result.Message = fmt.Sprintf("%v", message)
	}
	return result, nil, nil
}

func splitOutputLines(output string) []string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	trimmed := strings.TrimRight(output, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func asBool(value any) (bool, error) {
	return ParseBool(value)
}
