// Simulator for preflight terminal output. Not compiled into the main binary.
// Usage:
//
//	go run ./tools/sim [scenario] [--format tui|text|json] [--verbose] [--delay 100ms]
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bluecadet/preflight/internal/output"
)

type scenario struct {
	name        string
	description string
	run         func(r output.Renderer, delay time.Duration)
}

var scenarios []scenario

func init() {
	scenarios = []scenario{
		{
			name:        "basic",
			description: "single host, sequential tasks, all ok",
			run:         runBasic,
		},
		{
			name:        "multi-host",
			description: "three hosts running concurrently with mixed statuses",
			run:         runMultiHost,
		},
		{
			name:        "failures",
			description: "tasks that fail mid-play",
			run:         runFailures,
		},
		{
			name:        "nested",
			description: "nested tasks via slash-separated TaskIDs",
			run:         runNested,
		},
		{
			name:        "skipped",
			description: "tasks skipped for various reasons",
			run:         runSkipped,
		},
		{
			name:        "large",
			description: "stress test: 8 hosts, 12 tasks each",
			run:         runLarge,
		},
		{
			name:        "changed",
			description: "mix of ok and changed tasks",
			run:         runChanged,
		},
		{
			name:        "streaming",
			description: "tasks that emit streamed output while running",
			run:         runStreaming,
		},
		{
			name:        "streaming-multi-host",
			description: "multiple hosts emitting streamed output concurrently",
			run:         runStreamingMultiHost,
		},
		{
			name:        "roster",
			description: "multi-target run-start roster with mixed transports (ssh, winrm, local)",
			run:         runRoster,
		},
		{
			name:        "inline-prefixes",
			description: "inline target prefixes across mixed transports on finished + running tasks",
			run:         runInlinePrefixes,
		},
		{
			name:        "readme",
			description: "mixed-transport fleet rollout with streamed logs and randomized durations",
			run:         runReadme,
		},
	}
}

func main() {
	formatFlag := flag.String("format", "auto", "output format: auto, tui, text, json")
	verboseFlag := flag.Bool("verbose", false, "show logs for all completed tasks")
	delayFlag := flag.Duration("delay", 80*time.Millisecond, "simulated task duration")

	// Extract the scenario name (first non-flag arg) so that flags can appear
	// anywhere: `sim basic --delay 500ms` and `sim --delay 500ms basic` both work.
	scenarioName := ""
	filtered := os.Args[:1]
	for _, arg := range os.Args[1:] {
		if len(arg) > 0 && arg[0] != '-' && scenarioName == "" {
			scenarioName = arg
		} else {
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered
	flag.Parse()

	if scenarioName == "list" {
		fmt.Println("available scenarios:")
		for _, s := range scenarios {
			fmt.Printf("  %-16s %s\n", s.name, s.description)
		}
		return
	}

	format, err := parseFormat(*formatFlag, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if scenarioName == "" || scenarioName == "all" {
		for i, s := range scenarios {
			if i > 0 {
				time.Sleep(300 * time.Millisecond)
			}
			fmt.Printf("\n--- scenario: %s ---\n\n", s.name)
			r := output.NewWithOptions(format, os.Stdout, output.Options{Verbose: *verboseFlag})
			s.run(r, *delayFlag)
			r.Close()
		}
		return
	}

	for _, s := range scenarios {
		if s.name == scenarioName {
			r := output.NewWithOptions(format, os.Stdout, output.Options{Verbose: *verboseFlag})
			s.run(r, *delayFlag)
			r.Close()
			return
		}
	}

	// fuzzy match
	var matches []scenario
	for _, s := range scenarios {
		if containsFold(s.name, scenarioName) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		r := output.NewWithOptions(format, os.Stdout, output.Options{Verbose: *verboseFlag})
		matches[0].run(r, *delayFlag)
		r.Close()
		return
	}

	fmt.Fprintf(os.Stderr, "unknown scenario %q\nrun 'go run ./tools/sim list' to see available scenarios\n", scenarioName)
	os.Exit(1)
}

func parseFormat(raw string, w *os.File) (output.Format, error) {
	switch raw {
	case "auto":
		return output.AutoDetect(w), nil
	case "tui":
		return output.FormatTUI, nil
	case "text":
		return output.FormatText, nil
	case "json":
		return output.FormatJSON, nil
	default:
		return "", fmt.Errorf("unknown format %q", raw)
	}
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			ca, cb := s[i+j], sub[j]
			if ca >= 'A' && ca <= 'Z' {
				ca += 32
			}
			if cb >= 'A' && cb <= 'Z' {
				cb += 32
			}
			if ca != cb {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
