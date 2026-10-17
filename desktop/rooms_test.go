package desktop

import "testing"

func TestTagRoomsSetsOwningAccount(t *testing.T) {
	rooms := []Room{{ID: "!one:example.org", Name: "One"}, {ID: "!two:example.org", Name: "Two"}}

	tagRooms(rooms, "second")

	for _, room := range rooms {
		if room.AccountID != "second" {
			t.Fatalf("room %s account = %q, want %q", room.ID, room.AccountID, "second")
		}
	}
	if rooms[0].Name != "One" || rooms[1].ID != "!two:example.org" {
		t.Fatalf("tagRooms altered other fields: %+v", rooms)
	}
}
