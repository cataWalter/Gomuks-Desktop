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
