package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mychat/matrix"
)

type timelineModel struct {
	viewport   viewport.Model
	messages   []matrix.Message
	roomID     string
	hasMore    bool
	loading    bool
	selected   int
	lineOwners []int // message index of each rendered line
	media      map[string]mediaState
}

func newTimelineModel() timelineModel {
	vp := viewport.New(70, 20)
	return timelineModel{
		viewport: vp,
		messages: []matrix.Message{},
		media:    map[string]mediaState{},
	}
}

func (m timelineModel) Update(msg tea.Msg) (timelineModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m timelineModel) View() string {
	return m.viewport.View()
}

func (m *timelineModel) setMessages(messages []matrix.Message, hasMore bool) {
	selectedID := ""
	if current := m.selectedMessage(); current != nil && m.selected < len(m.messages)-1 {
		selectedID = current.ID
	}
	m.messages = messages
	m.hasMore = hasMore
	if len(messages) == 0 {
		m.selected = 0
	} else {
		m.selected = len(messages) - 1
		for i := range messages {
			if messages[i].ID == selectedID && selectedID != "" {
				m.selected = i
				break
			}
		}
	}
	m.viewport.SetContent(m.renderMessages())
	m.keepSelectionVisible()
}

func (m *timelineModel) selectedMessage() *matrix.Message {
	if m.selected < 0 || m.selected >= len(m.messages) {
		return nil
	}
	return &m.messages[m.selected]
}

func (m *timelineModel) moveSelection(delta int) {
	if len(m.messages) == 0 {
		return
	}
	m.selected = clampInt(m.selected+delta, 0, len(m.messages)-1)
	m.viewport.SetContent(m.renderMessages())
	m.keepSelectionVisible()
}

func (m *timelineModel) keepSelectionVisible() {
	line := slices.Index(m.lineOwners, m.selected)
	if line < 0 || m.viewport.Height < 1 {
		return
	}
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(line)
	} else if line >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(line - m.viewport.Height + 1)
	}
}

func (m timelineModel) messageAtLine(line int) (int, bool) {
	if line < 0 || line >= len(m.lineOwners) {
		return 0, false
	}
	return m.lineOwners[line], true
}

func (m *timelineModel) selectMessage(index int) {
	if index < 0 || index >= len(m.messages) {
		return
	}
	m.selected = index
	m.viewport.SetContent(m.renderMessages())
	m.keepSelectionVisible()
}

func (m *timelineModel) appendOwned(line string, owner int) {
	for range strings.Count(line, "\n") + 1 {
		m.lineOwners = append(m.lineOwners, owner)
	}
}

func (m *timelineModel) renderMessages() string {
	m.lineOwners = m.lineOwners[:0]
	if len(m.messages) == 0 {
		return "No messages"
	}
	var lines []string
	for i, msg := range m.messages {
		t := msg.Timestamp
		if t.IsZero() {
			t = time.Now()
		}
		ts := t.Format("15:04")
		sender := msg.SenderName
		if sender == "" {
			sender = msg.Sender
		}
		body := msg.Body
		if msg.SendError != "" {
			body = body + " [failed]"
		}
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		header := fmt.Sprintf("%s%s  %s   ", prefix, ts, sender)
		for _, line := range m.messageLines(header, body) {
			lines = append(lines, line)
			m.appendOwned(line, i)
		}
		if msg.ReplyToPreview != "" {
			reply := fmt.Sprintf("      → %s", msg.ReplyToPreview)
			lines = append(lines, reply)
			m.appendOwned(reply, i)
		}
		for _, row := range m.mediaLines(msg, m.posterCols()) {
			lines = append(lines, row)
			m.appendOwned(row, i)
		}
	}
	return strings.Join(lines, "\n")
}

// prependMessages adds older messages above the loaded ones and keeps the viewport on the same content.
func (m *timelineModel) prependMessages(older []matrix.Message, hasMore bool) {
	selectedID := ""
	if current := m.selectedMessage(); current != nil {
		selectedID = current.ID
	}
	known := make(map[string]bool, len(m.messages))
	for _, msg := range m.messages {
		known[msg.ID] = true
	}
	merged := make([]matrix.Message, 0, len(older)+len(m.messages))
	for _, msg := range older {
		if !known[msg.ID] {
			merged = append(merged, msg)
		}
	}
	merged = append(merged, m.messages...)
	linesBefore := len(m.lineOwners)
	m.messages = merged
	m.hasMore = hasMore
	m.selected = max(len(merged)-1, 0)
	for i := range merged {
		if merged[i].ID == selectedID && selectedID != "" {
			m.selected = i
			break
		}
	}
	m.viewport.SetContent(m.renderMessages())
	m.viewport.SetYOffset(m.viewport.YOffset + len(m.lineOwners) - linesBefore)
	m.keepSelectionVisible()
}

func (m timelineModel) oldestCursor() int64 {
	for _, msg := range m.messages {
		if msg.TimelineCursor > 0 {
			return msg.TimelineCursor
		}
	}
	return 0
}

// messageLines wraps body under header, indenting continuation rows to the body column.
func (m timelineModel) messageLines(header, body string) []string {
	indent := strings.Repeat(" ", lipgloss.Width(header))
	width := max(m.viewport.Width-lipgloss.Width(header), 10)
	wrapped := strings.Split(lipgloss.NewStyle().Width(width).Render(body), "\n")
	lines := make([]string, 0, len(wrapped))
	for i, part := range wrapped {
		part = strings.TrimRight(part, " ")
		if i == 0 {
			lines = append(lines, header+part)
		} else {
			lines = append(lines, indent+part)
		}
	}
	return lines
}

type TimelineMessagesMsg struct {
	RoomID   string
	Messages []matrix.Message
	HasMore  bool
}
