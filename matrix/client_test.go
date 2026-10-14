package matrix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
	"go.mau.fi/util/jsontime"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestMessageFromEvent(t *testing.T) {
	timestamp := time.Date(2026, time.October, 14, 17, 0, 0, 0, time.UTC)
	evt := &database.Event{
		ID:        id.EventID("$message"),
		Sender:    id.UserID("@alice:example.org"),
		Type:      event.EventMessage.Type,
		Content:   json.RawMessage(`{"msgtype":"m.text","body":"hello","formatted_body":"<strong>hello</strong>"}`),
		Timestamp: jsontime.UM(timestamp),
	}

	message, ok := messageFromEvent(evt)
	if !ok {
		t.Fatal("expected a Matrix text message")
	}
	if message.ID != "$message" || message.Sender != "@alice:example.org" || message.Body != "hello" {
		t.Fatalf("unexpected message snapshot: %+v", message)
	}
	if !message.Timestamp.Equal(timestamp) {
		t.Fatalf("timestamp = %s, want %s", message.Timestamp, timestamp)
	}
	if message.FormattedBody != "<strong>hello</strong>" {
		t.Fatalf("formatted body = %q", message.FormattedBody)
	}
}

func TestMessageFromEventMapsMediaMetadata(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantURI   string
		encrypted bool
	}{
		{
			name:    "unencrypted",
			content: `{"body":"photo","url":"mxc://example.org/plain","info":{"mimetype":"image/png"}}`,
			wantURI: "mxc://example.org/plain",
		},
		{
			name:      "encrypted",
			content:   `{"body":"photo","file":{"url":"mxc://example.org/encrypted","key":{},"iv":"iv","hashes":{},"v":"v2"}}`,
			wantURI:   "mxc://example.org/encrypted",
			encrypted: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evt := &database.Event{
				Type:    event.EventMessage.Type,
				Content: json.RawMessage(test.content),
			}
			message, ok := messageFromEvent(evt)
			if !ok {
				t.Fatal("expected a media message")
			}
			if message.MediaURI != test.wantURI {
				t.Errorf("media URI = %q, want %q", message.MediaURI, test.wantURI)
			}
			if (message.MediaEncrypted != nil) != test.encrypted {
				t.Errorf("encrypted metadata present = %t, want %t", message.MediaEncrypted != nil, test.encrypted)
			}
			if !test.encrypted && message.MediaMIME != "image/png" {
				t.Errorf("media MIME = %q, want image/png", message.MediaMIME)
			}
		})
	}
}

func TestMessageFromEventIgnoresNonMessages(t *testing.T) {
	tests := []struct {
		name string
		evt  *database.Event
	}{
		{
			name: "state event",
			evt:  &database.Event{Type: event.StateTopic.Type, StateKey: stringPointer("")},
		},
		{
			name: "message without body",
			evt:  &database.Event{Type: event.EventMessage.Type, Content: json.RawMessage(`{"msgtype":"m.text"}`)},
		},
		{
			name: "invalid content",
			evt:  &database.Event{Type: event.EventMessage.Type, Content: json.RawMessage(`invalid`)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if message, ok := messageFromEvent(test.evt); ok {
				t.Fatalf("unexpected message: %+v", message)
			}
		})
	}
}

func TestRoomNeedsReplyHonorsUnreadIncomingAndDismissedEvents(t *testing.T) {
	room := Room{LastEventID: "$incoming", LastEventSender: "@alice:example.org"}
	if !roomNeedsReply(room, "@me:example.org", "") {
		t.Fatal("incoming event should need a reply")
	}
	if roomNeedsReply(room, "@me:example.org", "$incoming") {
		t.Fatal("dismissed event should not need a reply")
	}
	room.LastEventID = "$next"
	if !roomNeedsReply(room, "@me:example.org", "$incoming") {
		t.Fatal("a new incoming event should reactivate the room")
	}
	room.LastEventSender = "@me:example.org"
	if roomNeedsReply(room, "@me:example.org", "") {
		t.Fatal("outgoing event with no unread messages should not need a reply")
	}
	room.Unread = 1
	if !roomNeedsReply(room, "@me:example.org", "") {
		t.Fatal("unread messages should put the room in needs-reply")
	}
	room = Room{ID: "!cached:example.org", Unread: 2}
	if roomNeedsReply(room, "@me:example.org", room.ID) {
		t.Fatal("a dismissed room with no locally known latest event should stay dismissed")
	}
}

func TestPublishCoalescesTimelineChangedRoomIDs(t *testing.T) {
	client := &Client{
		events: make(chan Update, 1),
		done:   make(chan struct{}),
	}
	client.publishTimeline([]string{"!first:example.org"}, "")
	client.publishTimeline([]string{"!second:example.org"}, "")

	update := <-client.events
	if len(update.TimelineChangedRoomID) != 2 {
		t.Fatalf("timeline changed rooms = %v, want both coalesced room IDs", update.TimelineChangedRoomID)
	}
	if update.TimelineChangedRoomID[0] != "!first:example.org" ||
		update.TimelineChangedRoomID[1] != "!second:example.org" {
		t.Fatalf("timeline changed rooms = %v, want sorted room IDs", update.TimelineChangedRoomID)
	}
	if len(update.ChangedRoomID) != 0 {
		t.Fatalf("metadata changed rooms = %v, want none", update.ChangedRoomID)
	}
}

func TestSyncStatusPublishesConnectionStates(t *testing.T) {
	client := &Client{
		events: make(chan Update, 4),
		done:   make(chan struct{}),
	}
	client.handleBackendEvent(&jsoncmd.SyncStatus{Type: jsoncmd.SyncStatusWaiting})
	if got := (<-client.events).ConnectionState; got != StateSyncing {
		t.Fatalf("waiting state = %q, want %q", got, StateSyncing)
	}
	client.handleBackendEvent(&jsoncmd.SyncStatus{Type: jsoncmd.SyncStatusOK})
	if update := <-client.events; update.ConnectionState != StateConnected || !update.InitialSyncComplete {
		t.Fatalf("connected update = %+v", update)
	}
	client.handleBackendEvent(&jsoncmd.SyncStatus{Type: jsoncmd.SyncStatusFailed})
	if update := <-client.events; update.ConnectionState != StateOffline || update.Error == "" {
		t.Fatalf("offline update = %+v", update)
	}
}

func TestApplySyncUpdatesRoomsAndRemovesLeftRooms(t *testing.T) {
	client := &Client{
		events: make(chan Update, 1),
		done:   make(chan struct{}),
		rooms: map[string]Room{
			"!left:example.org": {ID: "!left:example.org", Name: "Left room"},
		},
	}
	name := "Project chat"
	topic := "Planning"
	dmUser := id.UserID("@alice:example.org")
	avatar := id.ContentURIString("mxc://example.org/room-avatar").ParseOrIgnore()
	roomID := id.RoomID("!project:example.org")
	room := &database.Room{
		ID:       roomID,
		Name:     &name,
		Topic:    &topic,
		Avatar:   &avatar,
		DMUserID: &dmUser,
		UnreadCounts: database.UnreadCounts{
			UnreadMessages:      5,
			UnreadNotifications: 2,
			UnreadHighlights:    1,
		},
	}
	latest := time.Date(2026, time.October, 14, 17, 5, 0, 0, time.UTC)
	syncEvent := &jsoncmd.SyncComplete{
		Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
			roomID: {
				Meta: room,
				Events: []*database.Event{
					{
						Type:      event.EventMessage.Type,
						Content:   json.RawMessage(`{"body":"latest"}`),
						Timestamp: jsontime.UM(latest),
					},
				},
			},
		},
		LeftRooms: []id.RoomID{"!left:example.org"},
	}

	client.applySync(syncEvent, true)

	rooms := client.Rooms()
	if len(rooms) != 1 {
		t.Fatalf("room count = %d, want 1", len(rooms))
	}
	got := rooms[0]
	if got.ID != roomID.String() || got.Name != name || got.Topic != topic || got.Avatar != avatar.String() || got.Preview != "latest" {
		t.Fatalf("unexpected room snapshot: %+v", got)
	}
	if !got.Direct || got.Unread != 7 || got.Mentions != 1 {
		t.Fatalf("room direct/unread metadata was not mapped: %+v", got)
	}
	if !got.LastSeen.Equal(latest) {
		t.Fatalf("last seen = %s, want %s", got.LastSeen, latest)
	}
	update := <-client.Events()
	if len(update.ChangedRoomID) != 2 {
		t.Fatalf("changed rooms = %v, want update and removal", update.ChangedRoomID)
	}
}

func TestLocalMessagesMatchTimelineByEventOrTransactionID(t *testing.T) {
	tests := []struct {
		name      string
		timeline  Message
		candidate Message
	}{
		{
			name:      "event ID",
			timeline:  Message{ID: "$event"},
			candidate: Message{ID: "$event", TransactionID: "txn"},
		},
		{
			name:      "transaction ID",
			timeline:  Message{ID: "$event", TransactionID: "txn"},
			candidate: Message{ID: "~pending", TransactionID: "txn"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !containsMessage([]Message{test.timeline}, test.candidate) {
				t.Fatal("expected timeline message to match cached local message")
			}
		})
	}
}

func TestPublishCoalescesPendingUpdatesWithoutDroppingChangedRooms(t *testing.T) {
	client := &Client{
		events: make(chan Update, eventBufferSize),
		done:   make(chan struct{}),
		rooms:  make(map[string]Room),
		local:  make(map[string]map[string]Message),
	}

	for i := 0; i < eventBufferSize+100; i++ {
		client.publish([]string{fmt.Sprintf("!room-%03d:example.org", i)}, "")
	}

	updates := make([]Update, 0, eventBufferSize)
	for len(client.events) > 0 {
		updates = append(updates, <-client.events)
	}
	if len(updates) >= eventBufferSize {
		t.Fatalf("event queue was not coalesced: retained %d updates", len(updates))
	}
	changed := make(map[string]struct{})
	for _, update := range updates {
		for _, roomID := range update.ChangedRoomID {
			changed[roomID] = struct{}{}
		}
	}
	if len(changed) != eventBufferSize+100 {
		t.Fatalf("coalesced %d changed room IDs, want %d", len(changed), eventBufferSize+100)
	}
}

func TestInitCompletePublishesSyncReadyState(t *testing.T) {
	client := &Client{
		events: make(chan Update, 1),
		done:   make(chan struct{}),
		rooms:  make(map[string]Room),
		local:  make(map[string]map[string]Message),
	}

	client.handleBackendEvent(&jsoncmd.InitComplete{})

	select {
	case update := <-client.Events():
		if !update.InitialSyncComplete {
			t.Fatal("initial sync completion was not included in the update")
		}
	case <-time.After(time.Second):
		t.Fatal("expected a sync-ready update")
	}
	if !client.SyncReady() {
		t.Fatal("client should report initial sync ready")
	}
}

func TestNewInitializesAndReopensLocalDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "profile", "matrix.db")
	client, err := New(databasePath)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	client.Close()

	keyInfo, err := os.Stat(filepath.Join(filepath.Dir(databasePath), "crypto.key"))
	if err != nil {
		t.Fatalf("stat crypto key: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("crypto key permissions = %04o, want 0600", keyInfo.Mode().Perm())
	}

	client, err = New(databasePath)
	if err != nil {
		t.Fatalf("reopen client: %v", err)
	}
	client.Close()
}

func stringPointer(value string) *string {
	return &value
}
