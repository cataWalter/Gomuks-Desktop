package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mychat/matrix"
)

type RoomFilter int

const (
	FilterAll RoomFilter = iota
	FilterUnread
	FilterFavorite
	FilterNeedsReply
)

type roomsModel struct {
	rooms       []matrix.Room
	filter      RoomFilter
	selected    int
	offset      int
	rows        int
	lastUpdated map[string]int
}

func newRoomsModel() roomsModel {
	return roomsModel{
		rooms:       []matrix.Room{},
		filter:      FilterAll,
		selected:    0,
		lastUpdated: make(map[string]int),
	}
}

func (m roomsModel) filteredRooms() []matrix.Room {
	var result []matrix.Room
	for _, r := range m.rooms {
		switch m.filter {
		case FilterUnread:
			if r.Unread > 0 || r.Mentions > 0 {
				result = append(result, r)
			}
		case FilterFavorite:
			if r.Favorite {
				result = append(result, r)
			}
		case FilterNeedsReply:
			if r.Unanswered {
				result = append(result, r)
			}
		default:
			result = append(result, r)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		// Last activity first (most recent)
		if !result[i].LastEventAt.Equal(result[j].LastEventAt) {
			return result[i].LastEventAt.After(result[j].LastEventAt)
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func (m roomsModel) selectedRoom() *matrix.Room {
	filtered := m.filteredRooms()
	if len(filtered) == 0 || m.selected < 0 || m.selected >= len(filtered) {
		return nil
	}
	return &filtered[m.selected]
}

func (m roomsModel) Update(msg tea.Msg) (roomsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case UpdateRoomsMsg:
		m.rooms = msg.Rooms
	case FocusMsg:
		if msg.Focus == FocusRooms {
			// keep state
		}
	}
	return m, nil
}

// ensureVisible scrolls the room list so the selected room stays on screen.
func (m *roomsModel) ensureVisible() {
	rows := max(m.rows, 1)
	total := len(m.filteredRooms())
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+rows {
		m.offset = m.selected - rows + 1
	}
	m.offset = clampInt(m.offset, 0, max(total-rows, 0))
}

func (m roomsModel) View(width int) string {
	filtered := m.filteredRooms()
	if len(filtered) == 0 {
		return "No rooms"
	}

	var lines []string
	start := clampInt(m.offset, 0, len(filtered)-1)
	end := min(start+max(m.rows, 1), len(filtered))
	for i := start; i < end; i++ {
		r := filtered[i]
		prefix := "  "
		if i == m.selected {
			prefix = "❯ "
		}
		name := r.Name
		if name == "" {
			name = r.ID
		}
		badge := ""
		if r.Mentions > 0 {
			badge = fmt.Sprintf(" @%d", r.Mentions)
		} else if r.Unread > 0 {
			badge = fmt.Sprintf(" %d", r.Unread)
		}
		if r.Favorite {
			badge = " ★" + badge
		}
		if r.Unanswered {
			badge = badge + " !"
		}
		lines = append(lines, fitLine(prefix+name+badge, width))
	}
	return strings.Join(lines, "\n")
}

type UpdateRoomsMsg struct {
	Rooms []matrix.Room
}
