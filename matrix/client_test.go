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
		Content:   json.RawMessage(`{"msgtype":"m.text","body":"hello"}`),
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
	roomID := id.RoomID("!project:example.org")
	room := &database.Room{
		ID:    roomID,
		Name:  &name,
		Topic: &topic,
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
	if got.ID != roomID.String() || got.Name != name || got.Topic != topic || got.Preview != "latest" {
		t.Fatalf("unexpected room snapshot: %+v", got)
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
