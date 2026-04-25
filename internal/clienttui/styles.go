package clienttui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorText   = lipgloss.Color("#d9e0ee")
	colorMuted  = lipgloss.Color("#8b93a6")
	colorDim    = lipgloss.Color("#6b7280")
	colorAccent = lipgloss.Color("#7aa2f7")
	colorGreen  = lipgloss.Color("#9ece6a")
	colorYellow = lipgloss.Color("#e0af68")
	colorRed    = lipgloss.Color("#f7768e")
	colorBlue   = lipgloss.Color("#7dcfff")
	colorLine   = lipgloss.Color("#2a2f3a")
)

var (
	appStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(1, 2)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	versionStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText)

	keyStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorText)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	cursorStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	selectedTextStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Bold(true)

	rowTextStyle = lipgloss.NewStyle().
			Foreground(colorText)

	dividerStyle = lipgloss.NewStyle().
			Foreground(colorLine)

	progressBarFillStyle = lipgloss.NewStyle().
				Foreground(colorAccent)

	progressBarEmptyStyle = lipgloss.NewStyle().
				Foreground(colorLine)

	tableHeaderStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Bold(true)
)

func appHeader(version string) string {
	//noinspection SpellCheckingInspection
	return headerStyle.Render("MeguruPacks Client") + "  " + versionStyle.Render(version)
}

func renderDivider(width int) string {
	if width <= 0 {
		width = 72
	}
	return dividerStyle.Render(strings.Repeat("─", width))
}

func renderSectionTitle(title string) string {
	return sectionStyle.Render(title)
}

func renderTableHeader(text string) string {
	return tableHeaderStyle.Render(text)
}

func kv(label, value string) string {
	return keyStyle.Render(padRight(label, 16)) + valueStyle.Render(value)
}

func chipOK(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorGreen).
		Render("[" + text + "]")
}

func chipWarn(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorYellow).
		Render("[" + text + "]")
}

func chipInfo(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorBlue).
		Render("[" + text + "]")
}

func chipMuted(text string) string {
	return lipgloss.NewStyle().
		Foreground(colorMuted).
		Render("[" + text + "]")
}

func renderHint(text string) string {
	return footerStyle.Render(text)
}

func renderErrorBlock(errText string) string {
	return errorStyle.Render("Error: ") + errText
}

func renderInfoBlock(title, text string) string {
	return renderSectionTitle(title) + "\n" + mutedStyle.Render(text)
}

func renderProgressBar(percent float64, width int) string {
	if width <= 0 {
		width = 28
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int((percent / 100) * float64(width))
	if filled > width {
		filled = width
	}

	full := progressBarFillStyle.Render(strings.Repeat("█", filled))
	empty := progressBarEmptyStyle.Render(strings.Repeat("█", width-filled))
	return full + empty
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(r))
}

func humanBool(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}
