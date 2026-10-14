package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"mychat/matrix"
)

func TestChatViewPrependsOlderMessagesAndRefreshesLatest(t *testing.T) {
	view := NewChatView(nil)
	view.SetRoom(matrix.Room{ID: "!room:example.org", Name: "Room"})
	oldTime := time.Date(2026, time.October, 14, 17, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	view.SetHistoryPage(matrix.HistoryPage{
		Messages: []matrix.Message{{ID: "$new", Body: "new", Timestamp: newTime}},
		Before:   20,
		HasMore:  true,
	}, false)

	view.SetHistoryPage(matrix.HistoryPage{
		Messages: []matrix.Message{
			{ID: "$old", Body: "old", Timestamp: oldTime},
			{ID: "$new", Body: "duplicate", Timestamp: newTime},
		},
		Before: 10,
	}, true)
	if len(view.history) != 2 || view.history[0].ID != "$old" || view.history[1].Body != "new" {
		t.Fatalf("older page was not prepended and deduplicated: %+v", view.history)
	}
	if view.before != 10 || view.hasOlder {
		t.Fatalf("older paging state = before %d, hasOlder %v", view.before, view.hasOlder)
	}

	view.RefreshLatest(matrix.HistoryPage{
		Messages: []matrix.Message{
			{ID: "$new", Body: "edited", Timestamp: newTime},
			{ID: "$latest", Body: "latest", Timestamp: newTime.Add(time.Minute)},
		},
		Before:  30,
		HasMore: true,
	})
	if len(view.history) != 3 {
		t.Fatalf("history length after refresh = %d, want 3", len(view.history))
	}
	if view.history[0].ID != "$old" || view.history[1].Body != "edited" || view.history[2].ID != "$latest" {
		t.Fatalf("refresh did not update latest messages while preserving older history: %+v", view.history)
	}
	if view.before != 10 || !view.hasOlder {
		t.Fatalf("refresh reset older paging state: before %d, hasOlder %v", view.before, view.hasOlder)
	}
}

func TestChatViewRequestsOlderPageOnceAndRecoversAfterFailure(t *testing.T) {
	view := NewChatView(nil)
	view.SetRoom(matrix.Room{ID: "!room:example.org"})
	requests := 0
	var gotRoom string
	var gotCursor int64
	view.SetLoadOlderHandler(func(roomID string, before int64) {
		requests++
		gotRoom, gotCursor = roomID, before
	})
	view.SetHistoryPage(matrix.HistoryPage{
		Messages: []matrix.Message{{ID: "$new"}},
		Before:   -7,
		HasMore:  true,
	}, false)

	view.requestOlder()
	view.requestOlder()
	if requests != 1 || gotRoom != "!room:example.org" || gotCursor != -7 {
		t.Fatalf("older page requests = %d, room %q, cursor %d", requests, gotRoom, gotCursor)
	}
	if !view.loadingOlder {
		t.Fatal("expected view to remain in loading state until response")
	}
	view.OlderLoadFailed("temporary failure")
	if view.loadingOlder || view.status.Text != "temporary failure" || view.loadOlder.Disabled() {
		t.Fatalf("view did not recover after pagination failure")
	}
}

func TestShouldGroupConsecutiveMessages(t *testing.T) {
	start := time.Date(2026, time.October, 14, 17, 0, 0, 0, time.UTC)
	first := matrix.Message{Sender: "@alice:example.org", Timestamp: start}
	if !shouldGroupMessages(first, matrix.Message{Sender: first.Sender, Timestamp: start.Add(5 * time.Minute)}) {
		t.Fatal("messages from the same sender at the five-minute boundary should group")
	}
	if shouldGroupMessages(first, matrix.Message{Sender: first.Sender, Timestamp: start.Add(5*time.Minute + time.Second)}) {
		t.Fatal("messages more than five minutes apart should not group")
	}
	if shouldGroupMessages(first, matrix.Message{Sender: "@bob:example.org", Timestamp: start.Add(time.Minute)}) {
		t.Fatal("messages from different senders should not group")
	}
}

func TestChatViewExplainsMessageLoadingAndEmptyHistory(t *testing.T) {
	view := NewChatView(nil)
	view.SetRoom(matrix.Room{ID: "!room:example.org", Name: "Room"})
	if !view.empty.Visible() || view.empty.Text != "Loading messages for this chat..." {
		t.Fatalf("loading state = %q, visible %v", view.empty.Text, view.empty.Visible())
	}

	view.SetHistoryPage(matrix.HistoryPage{}, false)
	if !view.empty.Visible() || view.empty.Text != "No messages are available in this chat yet. New messages will appear here when they sync." {
		t.Fatalf("empty-history state = %q, visible %v", view.empty.Text, view.empty.Visible())
	}

	view.SetMessages([]matrix.Message{{ID: "$message", Body: "hello"}})
	if view.empty.Visible() {
		t.Fatal("empty-state label should be hidden when messages are available")
	}
}

func TestComposerEnterSendsAndShiftEnterAddsNewline(t *testing.T) {
	var sent string
	view := NewChatView(func(_, text string) { sent = text })
	view.SetRoom(matrix.Room{ID: "!room:example.org"})
	view.input.SetText("hello")
	view.input.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if sent != "hello" || view.input.Text != "" {
		t.Fatalf("Enter result: sent %q, remaining text %q", sent, view.input.Text)
	}

	view.input.SetText("first")
	view.input.CursorRow = 0
	view.input.CursorColumn = len([]rune(view.input.Text))
	view.input.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftRight})
	view.input.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	view.input.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftRight})
	if sent != "hello" || view.input.Text != "first\n" {
		t.Fatalf("Shift+Enter result: sent %q, text %q", sent, view.input.Text)
	}
}

func TestComposerCtrlEnterModeKeepsPlainEnterAsNewline(t *testing.T) {
	var sent string
	view := NewChatView(func(_, text string) { sent = text })
	view.SetRoom(matrix.Room{ID: "!room:example.org"})
	view.SetSendOnCtrlEnter(true)
	view.input.SetText("first")
	view.input.CursorRow = 0
	view.input.CursorColumn = len([]rune(view.input.Text))
	view.input.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if sent != "" || view.input.Text != "first\n" {
		t.Fatalf("plain Enter in Ctrl+Enter mode: sent %q, text %q", sent, view.input.Text)
	}
	if view.input.PlaceHolder != "Write a message... (Ctrl+Enter to send, Enter for a new line)" {
		t.Fatalf("Ctrl+Enter placeholder = %q", view.input.PlaceHolder)
	}
}
