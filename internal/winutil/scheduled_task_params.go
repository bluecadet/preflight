package winutil

import (
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"time"
)

// NormalizeScheduledTaskParams applies aliases and defaults so both the local
// Windows module and the WinRM implementation use the same semantics.
func NormalizeScheduledTaskParams(params map[string]any) (map[string]any, error) {
	cloned := maps.Clone(params)

	if execute, ok := cloned["command"]; ok {
		if _, exists := cloned["execute"]; !exists {
			cloned["execute"] = execute
		}
	}
	if runAs, ok := cloned["user"]; ok {
		if _, exists := cloned["run_as"]; !exists {
			cloned["run_as"] = runAs
		}
	}

	path, err := normalizeTaskPath(cloned["path"])
	if err != nil {
		return nil, err
	}
	cloned["path"] = path

	trigger := "startup"
	if rawTrigger, ok := cloned["trigger"]; ok && rawTrigger != nil {
		text, ok := rawTrigger.(string)
		if !ok {
			return nil, fmt.Errorf("scheduled_task trigger must be a string, got %T", rawTrigger)
		}
		trigger = strings.ToLower(strings.TrimSpace(text))
	}
	cloned["trigger"] = trigger

	runLevel := "least"
	if rawRunLevel, ok := cloned["run_level"]; ok && rawRunLevel != nil {
		text, ok := rawRunLevel.(string)
		if !ok {
			return nil, fmt.Errorf("scheduled_task run_level must be a string, got %T", rawRunLevel)
		}
		runLevel = strings.ToLower(strings.TrimSpace(text))
	}
	if runLevel != "least" && runLevel != "highest" {
		return nil, fmt.Errorf("scheduled_task run_level must be least or highest")
	}
	cloned["run_level"] = runLevel

	enabled := true
	if rawEnabled, ok := cloned["enabled"]; ok && rawEnabled != nil {
		value, err := ParseBool(rawEnabled)
		if err != nil {
			return nil, fmt.Errorf("scheduled_task enabled: %w", err)
		}
		enabled = value
	}
	cloned["enabled"] = enabled

	if rawDelay, ok := cloned["delay"]; ok && rawDelay != nil {
		delay, err := normalizeDelay(rawDelay)
		if err != nil {
			return nil, fmt.Errorf("scheduled_task delay: %w", err)
		}
		cloned["delay"] = delay
	}

	if startAt, ok := cloned["start_at"]; ok && startAt != nil {
		text, ok := startAt.(string)
		if !ok {
			return nil, fmt.Errorf("scheduled_task start_at must be a string, got %T", startAt)
		}
		cloned["start_at"] = strings.TrimSpace(text)
	} else if trigger == "daily" {
		cloned["start_at"] = "03:00"
	}

	return cloned, nil
}

// ValidateScheduledTaskParams checks the normalized scheduled task parameter
// contract shared by the local Windows module and the WinRM implementation.
func ValidateScheduledTaskParams(params map[string]any) error {
	ensure := "present"
	if rawEnsure, ok := params["ensure"]; ok && rawEnsure != nil {
		text, ok := rawEnsure.(string)
		if !ok {
			return fmt.Errorf("scheduled_task ensure must be a string, got %T", rawEnsure)
		}
		ensure = strings.ToLower(strings.TrimSpace(text))
	}
	if ensure != "present" && ensure != "absent" {
		return fmt.Errorf("scheduled_task ensure must be present or absent")
	}
	if ensure == "absent" {
		return nil
	}

	execute, ok := params["execute"].(string)
	if !ok || strings.TrimSpace(execute) == "" {
		return fmt.Errorf("scheduled_task execute is required")
	}

	trigger, _ := params["trigger"].(string)
	switch trigger {
	case "startup", "onlogon", "daily", "once":
	default:
		return fmt.Errorf("scheduled_task trigger %q is not supported", trigger)
	}

	if _, ok := params["delay"]; ok && params["delay"] != nil {
		if trigger != "startup" && trigger != "onlogon" {
			return fmt.Errorf("scheduled_task delay is only supported for startup and onlogon triggers")
		}
	}

	if trigger == "once" {
		startAt, _ := params["start_at"].(string)
		if strings.TrimSpace(startAt) == "" {
			return fmt.Errorf("scheduled_task start_at is required for once trigger")
		}
	}

	return nil
}

func normalizeTaskPath(raw any) (string, error) {
	if raw == nil {
		return "\\", nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("scheduled_task path must be a string, got %T", raw)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "\\", nil
	}
	if !strings.HasPrefix(text, "\\") {
		text = "\\" + text
	}
	if !strings.HasSuffix(text, "\\") {
		text += "\\"
	}
	return text, nil
}

func normalizeDelay(raw any) (string, error) {
	switch typed := raw.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return "", nil
		}
		if strings.HasPrefix(strings.ToUpper(text), "P") {
			return strings.ToUpper(text), nil
		}
		duration, err := time.ParseDuration(text)
		if err != nil {
			return "", err
		}
		return durationToISODuration(duration)
	case int:
		return durationToISODuration(time.Duration(typed) * time.Second)
	case int64:
		return durationToISODuration(time.Duration(typed) * time.Second)
	case float64:
		if math.Trunc(typed) != typed {
			return "", fmt.Errorf("delay seconds must be an integer, got %v", typed)
		}
		return durationToISODuration(time.Duration(int64(typed)) * time.Second)
	default:
		return "", fmt.Errorf("unsupported delay type %T", raw)
	}
}

func durationToISODuration(duration time.Duration) (string, error) {
	if duration <= 0 {
		return "", fmt.Errorf("duration must be greater than zero")
	}
	totalSeconds := int64(duration / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	var b strings.Builder
	b.WriteString("PT")
	if hours > 0 {
		b.WriteString(strconv.FormatInt(hours, 10))
		b.WriteByte('H')
	}
	if minutes > 0 {
		b.WriteString(strconv.FormatInt(minutes, 10))
		b.WriteByte('M')
	}
	if seconds > 0 || (hours == 0 && minutes == 0) {
		b.WriteString(strconv.FormatInt(seconds, 10))
		b.WriteByte('S')
	}
	return b.String(), nil
}

func normalizeIntegralValue(value any, bits int) (int64, error) {
	switch typed := value.(type) {
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case float64:
		if math.Trunc(typed) != typed {
			return 0, fmt.Errorf("expected integer, got %v", typed)
		}
		return int64(typed), nil
	case string:
		s := strings.TrimSpace(typed)
		switch strings.ToLower(s) {
		case "true":
			return 1, nil
		case "false":
			return 0, nil
		}
		parsed, err := strconv.ParseInt(s, 10, bits)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("expected integer-like value, got %T", value)
	}
}
