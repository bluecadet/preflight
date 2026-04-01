package output

import (
	"fmt"
	"io"
	"strings"
)

func RenderScreenText(w io.Writer, screen Screen) error {
	if len(screen.Tabs) == 0 {
		return renderScreenSectionText(w, screen.Command, screen.Subject, screen.Summary, screen.Content)
	}

	if screen.Command != "" {
		if _, err := fmt.Fprintln(w, screen.Command); err != nil {
			return err
		}
	}
	if screen.Subject != "" {
		if _, err := fmt.Fprintln(w, screen.Subject); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	for idx, tab := range screen.Tabs {
		if idx > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		title := tab.Label
		if tab.Meta != "" {
			title += " (" + tab.Meta + ")"
		}
		if _, err := fmt.Fprintln(w, title); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, strings.Repeat("-", len(title))); err != nil {
			return err
		}
		summary := append([]ScreenStat{}, screen.Summary...)
		summary = append(summary, tab.Content.Summary...)
		if err := renderScreenContentText(w, summary, tab.Content); err != nil {
			return err
		}
	}

	return nil
}

func renderScreenSectionText(w io.Writer, command, subject string, summary []ScreenStat, content ScreenContent) error {
	if command != "" {
		if _, err := fmt.Fprintln(w, command); err != nil {
			return err
		}
	}
	if subject != "" {
		if _, err := fmt.Fprintln(w, subject); err != nil {
			return err
		}
	}
	if command != "" || subject != "" {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return renderScreenContentText(w, summary, content)
}

func renderScreenContentText(w io.Writer, summary []ScreenStat, content ScreenContent) error {
	if len(summary) > 0 {
		parts := make([]string, 0, len(summary))
		for _, stat := range summary {
			if stat.Label == "" && stat.Value == "" {
				continue
			}
			if stat.Label != "" && stat.Value != "" {
				parts = append(parts, stat.Label+"="+stat.Value)
				continue
			}
			parts = append(parts, stat.Value)
		}
		if len(parts) > 0 {
			if _, err := fmt.Fprintln(w, strings.Join(parts, "  ")); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
	}

	switch content.Kind {
	case ScreenKindDocument:
		text := content.Document
		if text == "" {
			text = content.Empty
		}
		_, err := fmt.Fprintln(w, text)
		return err
	default:
		if len(content.Items) == 0 {
			_, err := fmt.Fprintln(w, content.Empty)
			return err
		}
		for idx, item := range content.Items {
			if idx > 0 {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			line := item.Title
			if item.Subtitle != "" {
				line += " (" + item.Subtitle + ")"
			}
			if item.Summary != "" {
				line += " - " + item.Summary
			}
			if _, err := fmt.Fprintf(w, "%s %s\n", statusGlyph(item.Status), line); err != nil {
				return err
			}
			for _, meta := range item.Meta {
				if _, err := fmt.Fprintln(w, "  "+meta); err != nil {
					return err
				}
			}
			if err := renderScreenLinesText(w, item.Preview); err != nil {
				return err
			}
			if err := renderScreenLinesText(w, item.Detail); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderScreenLinesText(w io.Writer, lines []ScreenLine) error {
	for _, line := range lines {
		prefix := ""
		if line.Prefix != "" {
			prefix = line.Prefix + "> "
		}
		if _, err := fmt.Fprintln(w, "  "+prefix+line.Text); err != nil {
			return err
		}
	}
	return nil
}
