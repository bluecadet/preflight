package output

import "github.com/charmbracelet/lipgloss"

// maxLiveLines is the threshold above which output previews are hidden (dense mode).
const maxLiveLines = 8

// maxTaskPreviewLines is the number of recent output lines shown for an active task.
const maxTaskPreviewLines = 3

var (
	tsOK        = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "10"})
	tsChanged   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "3", Dark: "11"})
	tsFailed    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "9"})
	tsSkipped   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsMuted     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsBold      = lipgloss.NewStyle().Bold(true)
	tsSpin      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"})
	tsDivider   = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	tsOutput    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	tsElapsed   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tsCardTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "12"})
	tsLabel     = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	tsKey       = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	tsValue     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	tsTableHead = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	tsTableRule = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	tsRowInset       = lipgloss.NewStyle().PaddingLeft(2)
	tsCardTitleInset = lipgloss.NewStyle().PaddingLeft(2)
	tsCardBodyInset  = lipgloss.NewStyle().PaddingLeft(2)
)

// noColorifyStyles replaces all foreground-bearing TUI styles with versions
// that have no foreground color. Called once at model construction when the
// color mode is Never.
func noColorifyStyles() {
	tsOK = tsOK.Foreground(lipgloss.NoColor{})
	tsChanged = tsChanged.Foreground(lipgloss.NoColor{})
	tsFailed = tsFailed.Foreground(lipgloss.NoColor{})
	tsSkipped = tsSkipped.Foreground(lipgloss.NoColor{})
	tsMuted = tsMuted.Foreground(lipgloss.NoColor{})
	tsBold = tsBold.Foreground(lipgloss.NoColor{})
	tsSpin = tsSpin.Foreground(lipgloss.NoColor{})
	tsDivider = tsDivider.Foreground(lipgloss.NoColor{})
	tsOutput = tsOutput.Foreground(lipgloss.NoColor{})
	tsElapsed = tsElapsed.Foreground(lipgloss.NoColor{})
	tsCardTitle = tsCardTitle.Foreground(lipgloss.NoColor{})
	tsLabel = tsLabel.Foreground(lipgloss.NoColor{})
	tsKey = tsKey.Foreground(lipgloss.NoColor{})
	tsValue = tsValue.Foreground(lipgloss.NoColor{})
	tsTableHead = tsTableHead.Foreground(lipgloss.NoColor{})
	tsTableRule = tsTableRule.Foreground(lipgloss.NoColor{})
}
