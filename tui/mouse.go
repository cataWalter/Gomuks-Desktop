package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// loginFieldRows are the rows of the three login inputs within the login view.
var loginFieldRows = [...]int{3, 4, 5}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.showHelp || m.viewer != nil {
		return m, nil
	}
	if m.loginActive {
		if index, ok := m.loginFieldAt(msg.X, msg.Y); ok && isLeftPress(msg) {
			m.login.setFocus(index)
		}
		return m, nil
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		m.scrollAt(msg.X, msg.Y, -1)
	case msg.Button == tea.MouseButtonWheelDown:
		m.scrollAt(msg.X, msg.Y, 1)
	case isLeftPress(msg):
		m.clickAt(msg.X, msg.Y)
	}
	return m, nil
}

func isLeftPress(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
}

func (m Model) loginFieldAt(x, y int) (int, bool) {
	view := m.login.View()
	width, height := lipgloss.Width(view), lipgloss.Height(view)
	left := max(0, (m.width-width)/2)
	top := max(0, (m.height-height)/2)
	if x < left || x >= left+width {
		return 0, false
	}
	for index, row := range loginFieldRows {
		if y == top+row {
			return index, true
		}
	}
	return 0, false
}

func (m *Model) scrollAt(x, y, direction int) {
	focus, ok := m.panelAt(x, y)
	if !ok {
		return
	}
	switch focus {
	case FocusRooms:
		m.moveRoom(direction)
	case FocusTimeline:
		if direction < 0 {
			m.timeline.viewport.ScrollUp(m.timeline.viewport.MouseWheelDelta)
		} else {
			m.timeline.viewport.ScrollDown(m.timeline.viewport.MouseWheelDelta)
		}
	}
}

func (m *Model) clickAt(x, y int) {
	focus, ok := m.panelAt(x, y)
	if !ok {
		return
	}
	lay := m.layout()
	switch focus {
	case FocusRooms:
		m.setFocus(FocusRooms)
		if inner := lay.rooms.inner(); inner.contains(x, y) {
			m.selectRoomAt(m.rooms.offset + y - inner.y)
		}
	case FocusTimeline:
		m.setFocus(FocusTimeline)
		inner := lay.timeline.inner()
		if !inner.contains(x, y) {
			return
		}
		line := m.timeline.viewport.YOffset + y - inner.y
		if index, ok := m.timeline.messageAtLine(line); ok {
			m.timeline.selectMessage(index)
		}
	case FocusComposer:
		m.setFocus(FocusComposer)
	}
}

// selectRoomAt opens the room at index in the filtered list, as Enter does for the selection.
func (m *Model) selectRoomAt(index int) {
	filtered := m.rooms.filteredRooms()
	if index < 0 || index >= len(filtered) {
		return
	}
	m.rooms.selected = index
	m.rooms.ensureVisible()
	m.selectRoom(filtered[index])
	m.setFocus(FocusTimeline)
}
