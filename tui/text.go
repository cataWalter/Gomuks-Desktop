package tui

import "github.com/charmbracelet/lipgloss"

// fitLine truncates s to width columns so padded panels never wrap it onto extra rows.
func fitLine(s string, width int) string {
	return lipgloss.NewStyle().MaxWidth(max(width, 1)).Render(s)
}
