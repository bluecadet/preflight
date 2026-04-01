package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAutoDetect_NonTTY(t *testing.T) {
	var buf bytes.Buffer
	f := AutoDetect(&buf)
	if f != FormatText {
		t.Errorf("AutoDetect with bytes.Buffer: expected FormatText, got %q", f)
	}
}

func TestAutoDetect_AnotherNonTTY(t *testing.T) {
	w := &bytes.Buffer{}
	got := AutoDetect(w)
	if got != FormatText {
		t.Errorf("expected FormatText for non-TTY writer, got %q", got)
	}
}

func TestStaticScreenModel_ViewRespectsWindowHeight(t *testing.T) {
	model := newStaticScreenModel(Screen{
		Command: "plan",
		Subject: "play: demo",
		Status:  "ready",
		Content: ScreenContent{
			Kind: ScreenKindList,
			Items: []ScreenItem{
				{Title: "one", Status: "ok"},
				{Title: "two", Status: "ok"},
				{Title: "three", Status: "ok"},
				{Title: "four", Status: "ok"},
			},
		},
	})
	model.width = 60
	model.height = 10
	model.initialized = true

	rendered := model.View()
	if lipgloss.Height(rendered) > model.height {
		t.Fatalf("expected static rendered view height <= %d, got %d\n%s", model.height, lipgloss.Height(rendered), rendered)
	}
}

func TestRenderTabsUsesPaginatorWhenNeeded(t *testing.T) {
	tabs := []tuiTab{
		{Label: "host-a", Status: "complete", Meta: "8/8"},
		{Label: "host-b", Status: "running", Meta: "4/8"},
		{Label: "host-c", Status: "pending", Meta: "0/8"},
	}
	pager := newTUITabPager()

	rendered := renderTabs(tabs, 2, 24, &pager)
	if !strings.Contains(rendered, "host-c") {
		t.Fatalf("expected active tab page to include host-c, got %q", rendered)
	}
	if !strings.Contains(rendered, pager.ActiveDot) {
		t.Fatalf("expected paginator dots in narrow tab view, got %q", rendered)
	}
}
