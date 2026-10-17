package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mychat/matrix"
)

func mouseTestModel(width, height int) Model {
	model := NewModel(nil)
	model.loginActive = false
	rooms := make([]matrix.Room, 0, 5)
	for i := range 5 {
		rooms = append(rooms, matrix.Room{
			ID:          fmt.Sprintf("!room%d:example.org", i),
			Name:        fmt.Sprintf("Room %d", i),
			LastEventAt: time.Unix(int64(1000-i), 0),
		})
	}
	model.updateRooms(rooms)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

func mouseClick(model Model, x, y int) Model {
	updated, _ := model.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return updated.(Model)
}

func mouseWheel(model Model, x, y int, button tea.MouseButton) Model {
	updated, _ := model.Update(tea.MouseMsg{X: x, Y: y, Button: button})
	return updated.(Model)
}

func TestMouseClickOpensRoom(t *testing.T) {
	model := mouseTestModel(100, 30)
	inner := model.layout().rooms.inner()
	model = mouseClick(model, inner.x+1, inner.y+2)
	if model.rooms.selected != 2 || model.currentRoom == nil || model.currentRoom.ID != "!room2:example.org" {
		t.Fatalf("selected = %d, room = %+v, want room 2", model.rooms.selected, model.currentRoom)
	}
	if model.focus != FocusTimeline {
		t.Fatalf("focus = %v, want timeline", model.focus)
	}
}

func TestMouseClickOpensRoomInCompactLayout(t *testing.T) {
	model := mouseTestModel(60, 16)
	inner := model.layout().rooms.inner()
	model = mouseClick(model, inner.x+1, inner.y+1)
	if model.currentRoom == nil || model.currentRoom.ID != "!room1:example.org" {
		t.Fatalf("room = %+v, want room 1", model.currentRoom)
	}
}

func TestMouseClickSelectsTimelineMessage(t *testing.T) {
	model := mouseTestModel(100, 30)
	model.timeline.setMessages([]matrix.Message{
		{ID: "$one", Body: "first"},
		{ID: "$two", Body: "second"},
		{ID: "$three", Body: "third"},
	}, false)
	inner := model.layout().timeline.inner()
	model = mouseClick(model, inner.x+1, inner.y+1)
	if selected := model.timeline.selectedMessage(); selected == nil || selected.ID != "$two" {
		t.Fatalf("selected message = %+v, want $two", selected)
	}
}

func TestMouseWheelScrollsTimeline(t *testing.T) {
	model := mouseTestModel(100, 30)
	messages := make([]matrix.Message, 0, 30)
	for i := range 30 {
		messages = append(messages, matrix.Message{ID: fmt.Sprintf("$m%02d", i), Body: fmt.Sprintf("message %02d", i)})
	}
	model.timeline.setMessages(messages, false)
	before := model.timeline.viewport.YOffset
	inner := model.layout().timeline.inner()
	model = mouseWheel(model, inner.x+1, inner.y+1, tea.MouseButtonWheelUp)
	if got := model.timeline.viewport.YOffset; got != before-3 {
		t.Fatalf("YOffset after wheel up = %d, want %d", got, before-3)
	}
}

func TestMouseWheelMovesRoomSelection(t *testing.T) {
	model := mouseTestModel(100, 30)
	inner := model.layout().rooms.inner()
	model = mouseWheel(model, inner.x+1, inner.y+1, tea.MouseButtonWheelDown)
	if model.rooms.selected != 1 {
		t.Fatalf("selected = %d, want 1", model.rooms.selected)
	}
}

func TestMouseClickFocusesComposer(t *testing.T) {
	model := mouseTestModel(100, 30)
	inner := model.layout().composer.inner()
	model = mouseClick(model, inner.x+1, inner.y)
	if model.focus != FocusComposer {
		t.Fatalf("focus = %v, want composer", model.focus)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if got := updated.(Model).composer.textarea.Value(); got != "x" {
		t.Fatalf("composer text = %q, want x", got)
	}
}

func TestMouseClickSelectsLoginField(t *testing.T) {
	model := *NewModel(nil)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(Model)
	for y, line := range strings.Split(model.View(), "\n") {
		if x := strings.Index(line, "Password"); x >= 0 {
			model = mouseClick(model, x+1, y)
			break
		}
	}
	if model.login.focus != 2 {
		t.Fatalf("login focus = %d, want password field", model.login.focus)
	}
}
