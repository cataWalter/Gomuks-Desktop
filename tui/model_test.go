package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mychat/matrix"
)

func TestRoomsFiltered(t *testing.T) {
	rm := newRoomsModel()
	rm.rooms = []matrix.Room{
		{ID: "1", Name: "alpha", Unread: 0, Favorite: false},
		{ID: "2", Name: "beta", Unread: 5, Favorite: false},
		{ID: "3", Name: "gamma", Unread: 0, Favorite: true},
	}
	rm.filter = FilterUnread
	fr := rm.filteredRooms()
	if len(fr) != 1 || fr[0].Name != "beta" {
		t.Fatalf("expected 1 beta, got %+v", fr)
	}
	rm.filter = FilterFavorite
	fr = rm.filteredRooms()
	if len(fr) != 1 || fr[0].Name != "gamma" {
		t.Fatalf("expected 1 gamma, got %+v", fr)
	}
	rm.filter = FilterAll
	fr = rm.filteredRooms()
	if len(fr) != 3 {
		t.Fatalf("expected 3, got %d", len(fr))
	}
}

func TestUnreadFilterShortcutSelectsMatchingRoom(t *testing.T) {
	model := NewModel(nil)
	model.loginActive = false
	model.updateRooms([]matrix.Room{
		{ID: "!alpha:example.org", Name: "alpha", Favorite: true},
		{ID: "!beta:example.org", Name: "beta", Unread: 2},
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	result := updated.(Model)
	if result.rooms.filter != FilterUnread {
		t.Fatalf("filter = %v, want unread", result.rooms.filter)
	}
	if result.currentRoom == nil || result.currentRoom.ID != "!beta:example.org" {
		t.Fatalf("selected room = %+v, want beta", result.currentRoom)
	}
}

func TestComposerAcceptsQuitAndDigitKeys(t *testing.T) {
	model := *NewModel(nil)
	model.loginActive = false
	model.setFocus(FocusComposer)
	for _, input := range []rune{'q', '1'} {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{input}})
		model = updated.(Model)
	}
	if got := model.composer.textarea.Value(); got != "q1" {
		t.Fatalf("composer text = %q, want q1", got)
	}
}

func TestLayoutResizesForWideAndNarrowTerminals(t *testing.T) {
	model := NewModel(nil)
	model.loginActive = false
	wide, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	wideModel := wide.(Model)
	if wideModel.timeline.viewport.Width < 40 || wideModel.timeline.viewport.Height < 10 {
		t.Fatalf("wide viewport dimensions = %dx%d", wideModel.timeline.viewport.Width, wideModel.timeline.viewport.Height)
	}
	if got := lipgloss.Width(wideModel.View()); got > 120 {
		t.Fatalf("wide layout width = %d, want at most 120", got)
	}

	narrow, _ := wideModel.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	narrowModel := narrow.(Model)
	if narrowModel.timeline.viewport.Width > 60 || narrowModel.timeline.viewport.Height > 16 {
		t.Fatalf("narrow viewport dimensions = %dx%d", narrowModel.timeline.viewport.Width, narrowModel.timeline.viewport.Height)
	}
	_ = narrowModel.View()
}

func TestTimelineSelectionIsStableAcrossRefresh(t *testing.T) {
	timeline := newTimelineModel()
	initial := []matrix.Message{
		{ID: "$first", Body: "first"},
		{ID: "$second", Body: "second"},
	}
	timeline.setMessages(initial, false)
	timeline.moveSelection(-1)
	timeline.setMessages(append(initial, matrix.Message{ID: "$third", Body: "third"}), false)
	if selected := timeline.selectedMessage(); selected == nil || selected.ID != "$first" {
		t.Fatalf("selected message after refresh = %+v, want first", selected)
	}
}

func TestReplyAndEditUseSelectedTimelineMessage(t *testing.T) {
	model := *NewModel(nil)
	model.loginActive = false
	model.currentRoom = &matrix.Room{ID: "!room:example.org"}
	model.timeline.setMessages([]matrix.Message{
		{ID: "$reply", Body: "please answer"},
		{ID: "$own", Body: "my message", Own: true},
	}, false)
	model.timeline.moveSelection(-1)
	model.setFocus(FocusTimeline)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	replyModel := updated.(Model)
	if replyModel.composer.mode != ModeReply || replyModel.composer.replyTo == nil || replyModel.composer.replyTo.ID != "$reply" {
		t.Fatalf("reply target = %+v, want $reply", replyModel.composer.replyTo)
	}

	model = *NewModel(nil)
	model.loginActive = false
	model.currentRoom = &matrix.Room{ID: "!room:example.org"}
	model.timeline.setMessages([]matrix.Message{{ID: "$own", Body: "my message", Own: true}}, false)
	model.setFocus(FocusTimeline)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	editModel := updated.(Model)
	if editModel.composer.mode != ModeEdit || editModel.composer.editMsg == nil || editModel.composer.editMsg.ID != "$own" {
		t.Fatalf("edit target = %+v, want $own", editModel.composer.editMsg)
	}
}

func TestLoginSubmissionMasksPassword(t *testing.T) {
	login := newLoginModel()
	login.homeserver.SetValue("https://matrix.example")
	login.username.SetValue("@alice:matrix.example")
	login.password.SetValue("s3cret")
	login.setFocus(2)

	login, submit := login.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submit == nil {
		t.Fatal("expected login submission command")
	}
	message, ok := submit().(LoginSubmitMsg)
	if !ok {
		t.Fatalf("submission message = %T, want LoginSubmitMsg", submit())
	}
	if message.Homeserver != "https://matrix.example" || message.Username != "@alice:matrix.example" || message.Password != "s3cret" {
		t.Fatalf("unexpected login submission: %+v", message)
	}
	if strings.Contains(login.View(), "s3cret") {
		t.Fatal("login view rendered the password in plain text")
	}
}

func TestViewFitsTerminalAndKeepsSelectedRoomVisible(t *testing.T) {
	model := NewModel(nil)
	model.loginActive = false
	rooms := make([]matrix.Room, 0, 60)
	for i := range 60 {
		rooms = append(rooms, matrix.Room{
			ID:          fmt.Sprintf("!room%02d:example.org", i),
			Name:        fmt.Sprintf("Room %02d with a long display name", i),
			LastEventAt: time.Unix(int64(1000-i), 0),
		})
	}
	model.updateRooms(rooms)
	model.timeline.setMessages([]matrix.Message{{ID: "$long", Body: strings.Repeat("word ", 120)}}, false)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	current := updated
	if got := lipgloss.Height(current.View()); got > 30 {
		t.Fatalf("view height = %d, want at most 30", got)
	}
	for range 40 {
		current, _ = current.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	view := current.View()
	if got := lipgloss.Height(view); got > 30 {
		t.Fatalf("view height after scrolling = %d, want at most 30", got)
	}
	if got := lipgloss.Width(view); got > 100 {
		t.Fatalf("view width = %d, want at most 100", got)
	}
	if !strings.Contains(view, "❯ Room 40 with") {
		t.Fatalf("selected room is not visible after scrolling:\n%s", view)
	}
}
