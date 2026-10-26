package target

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf16"
)

func buildEncodedPowerShellCommand(binary, script string) string {
	encoded := encodePowerShellScript(script)
	return shellQuoteExec(binary, []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", encoded})
}

func encodePowerShellScript(script string) string {
	codeUnits := utf16.Encode([]rune(script))
	buf := make([]byte, len(codeUnits)*2)
	for i, unit := range codeUnits {
		buf[2*i] = byte(unit)
		buf[2*i+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
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
