package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"mychat/matrix"
)

func TestRoomListKeepsSelectionWhenRoomsReorder(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()
	window := testApp.NewWindow("room list test")

	selected := 0
	roomList := NewRoomList(func(matrix.Room) { selected++ })
	window.SetContent(roomList.Object())
	roomList.SetRooms([]matrix.Room{
		{ID: "!a:example.org", Name: "A"},
		{ID: "!b:example.org", Name: "B"},
	})
	roomList.SelectRoom("!b:example.org")
	if selected != 0 {
		t.Fatalf("programmatic selection invoked the user callback %d times", selected)
	}

	roomList.SetRooms([]matrix.Room{
		{ID: "!b:example.org", Name: "B"},
		{ID: "!a:example.org", Name: "A"},
	})
	if roomList.selected != "!b:example.org" {
		t.Fatalf("selected room = %q after reorder, want !b:example.org", roomList.selected)
	}
	if selected != 0 {
		t.Fatalf("reordering invoked the user callback %d times", selected)
	}

	roomList.SetRooms([]matrix.Room{{ID: "!a:example.org", Name: "A"}})
	if roomList.selected != "" {
		t.Fatalf("selection = %q after selected room was removed, want empty", roomList.selected)
	}
}

func TestRoomListShowsSyncingAndNoChatsStates(t *testing.T) {
	roomList := NewRoomList(nil)
	roomList.SetRooms(nil)
	if !roomList.empty.Visible() {
		t.Fatal("empty room list should show a placeholder")
	}

	if roomList.empty.Text != "Syncing your chats from the homeserver. Large accounts may take a little while." {
		t.Fatalf("syncing state = %q", roomList.empty.Text)
	}

	roomList.SetSyncReady(true)
	if roomList.empty.Text != "No chats yet. Rooms you join will appear here." {
		t.Fatalf("ready empty state = %q", roomList.empty.Text)
	}

	roomList.SetRooms([]matrix.Room{{ID: "!room:example.org", Name: "Room"}})
	if roomList.empty.Visible() {
		t.Fatal("empty-state label should be hidden when rooms are available")
	}
}

func TestRoomListFiltersByChatTypeAndQuery(t *testing.T) {
	rooms := NewRoomList(nil)
	rooms.SetRooms([]matrix.Room{
		{ID: "!dm:example.org", Name: "Alice", Preview: "hello", Direct: true},
		{ID: "!group:example.org", Name: "Project", Preview: "planning"},
	})

	rooms.setFilter(directRooms)
	if len(rooms.visible) != 1 || rooms.visible[0].ID != "!dm:example.org" {
		t.Fatalf("DM filter returned %+v", rooms.visible)
	}

	rooms.setFilter(groupRooms)
	if len(rooms.visible) != 1 || rooms.visible[0].ID != "!group:example.org" {
		t.Fatalf("group filter returned %+v", rooms.visible)
	}

	rooms.setFilter(allRooms)
	rooms.query = "PLANNING"
	rooms.refresh()
	if len(rooms.visible) != 1 || rooms.visible[0].ID != "!group:example.org" {
		t.Fatalf("case-insensitive preview search returned %+v", rooms.visible)
	}
	if rooms.empty.Visible() {
		t.Fatal("empty-state placeholder should be hidden while a room matches")
	}
	rooms.query = "missing"
	rooms.refresh()
	if len(rooms.visible) != 0 || !rooms.empty.Visible() || rooms.empty.Text != "No chats match this search or filter." {
		t.Fatalf("no-match state = visible %v, text %q", rooms.empty.Visible(), rooms.empty.Text)
	}
}

func TestRoomListNeedsReplyFilterAndAdvance(t *testing.T) {
	rooms := NewRoomList(nil)
	rooms.SetRooms([]matrix.Room{
		{ID: "!one:example.org", Name: "One", Unanswered: true},
		{ID: "!two:example.org", Name: "Two", Unanswered: false},
		{ID: "!three:example.org", Name: "Three", Unanswered: true},
	})
	if rooms.unanswered.Text != "Needs reply (2)" {
		t.Fatalf("needs-reply count label = %q", rooms.unanswered.Text)
	}
	rooms.setFilter(unansweredRooms)
	if len(rooms.visible) != 2 || rooms.visible[0].ID != "!one:example.org" || rooms.visible[1].ID != "!three:example.org" {
		t.Fatalf("needs-reply filter returned %+v", rooms.visible)
	}
	if next := rooms.NextVisibleRoom("!one:example.org"); next != "!three:example.org" {
		t.Fatalf("next visible room = %q, want !three:example.org", next)
	}
}
