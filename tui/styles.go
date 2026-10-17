package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Light-ish palette
	bg      = lipgloss.Color("255") // white background
	fg      = lipgloss.Color("233") // dark gray
	subtle  = lipgloss.Color("245") // medium gray
	border  = lipgloss.Color("237")
	accent  = lipgloss.Color("62")  // purple-ish
	unread  = lipgloss.Color("33")  // blue
	mention = lipgloss.Color("196") // red

	// Header
	headerStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(fg).
			Background(lipgloss.Color("252")).
			Bold(true)

	// Sidebar
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1).
			Width(30)

	// Timeline
	timelineStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1).
			Width(70)

	// Composer
	composerStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(border).
			Padding(0, 1)

	// Footer/status
	footerStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(subtle)
)
