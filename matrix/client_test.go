package matrix

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
	"go.mau.fi/util/jsontime"
	"maunium.net/go/mautrix"
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

func TestTimelineMessagePrefixCountsVisibleMessages(t *testing.T) {
	events := []*database.Event{
		{Type: event.EventReaction.Type, RedactedBy: "$redaction"},
		{ID: "$first", Type: event.EventMessage.Type, Content: json.RawMessage(`{"body":"first"}`)},
		{ID: "$second", Type: event.EventMessage.Type, Content: json.RawMessage(`{"body":"second"}`)},
	}

	prefix, count := timelineMessagePrefix(events, 1)
	if prefix != 2 || count != 1 {
		t.Fatalf("timeline prefix = %d with %d messages, want 2 with 1 message", prefix, count)
	}
}

func TestHasServerHistory(t *testing.T) {
	tests := []struct {
		name string
		room *database.Room
		want bool
	}{
		{name: "missing room"},
		{name: "no pagination token", room: &database.Room{}},
		{name: "pagination complete", room: &database.Room{PrevBatch: database.PrevBatchPaginationComplete}},
		{name: "server history available", room: &database.Room{PrevBatch: "opaque-next-page-token"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasServerHistory(test.room); got != test.want {
				t.Fatalf("hasServerHistory() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestHistoryPageReportsServerBackfill(t *testing.T) {
	client, err := New(filepath.Join(t.TempDir(), "profile", "matrix.db"))
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	roomID := id.RoomID("!history:example.org")
	if err = client.engine.DB.Room.CreateRow(ctx, roomID); err != nil {
		t.Fatalf("create room row: %v", err)
	}
	if err = client.engine.DB.Room.SetPrevBatch(ctx, roomID, "opaque-next-page-token"); err != nil {
		t.Fatalf("set room pagination token: %v", err)
	}

	page, err := client.HistoryPageWithLimit(ctx, roomID.String(), 0, 25)
	if err != nil {
		t.Fatalf("load initial history page: %v", err)
	}
	if !page.HasMore {
		t.Fatal("initial history page should expose server-side older messages")
	}
}

func TestMessageFromEventMapsMediaMetadata(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantURI    string
		wantMIME   string
		wantBody   string
		wantWidth  int
		wantHeight int
		encrypted  bool
	}{
		{
			name:       "unencrypted",
			content:    `{"body":"photo","url":"mxc://example.org/plain","info":{"mimetype":"image/png","w":640,"h":480}}`,
			wantURI:    "mxc://example.org/plain",
			wantMIME:   "image/png",
			wantBody:   "photo",
			wantWidth:  640,
			wantHeight: 480,
		},
		{
			name:      "encrypted",
			content:   `{"body":"photo","file":{"url":"mxc://example.org/encrypted","key":{},"iv":"iv","hashes":{},"v":"v2"}}`,
			wantURI:   "mxc://example.org/encrypted",
			encrypted: true,
		},
		{
			name:      "encrypted image without info",
			content:   `{"msgtype":"m.image","body":"photo.png","file":{"url":"mxc://example.org/image-without-info","key":{},"iv":"iv","hashes":{},"v":"v2"}}`,
			wantURI:   "mxc://example.org/image-without-info",
			wantMIME:  "image/unknown",
			encrypted: true,
		},
		{
			name: "Beeper gallery image and video",
			content: `{"msgtype":"com.beeper.gallery","body":"Sent an album with 1 image and 1 video:","com.beeper.gallery.images":[` +
				`{"msgtype":"m.image","body":"photo.jpg","file":{"url":"mxc://example.org/gallery-image","key":{},"iv":"iv","hashes":{},"v":"v2"},"info":{"mimetype":"image/jpeg","w":640,"h":480}},` +
				`{"msgtype":"m.video","body":"clip.mp4","file":{"url":"mxc://example.org/gallery-video","key":{},"iv":"iv","hashes":{},"v":"v2"}}]}`,
			wantURI:    "mxc://example.org/gallery-image",
			wantMIME:   "image/jpeg",
			wantBody:   "Sent an album with 1 image and 1 video:",
			wantWidth:  640,
			wantHeight: 480,
			encrypted:  true,
		},
		{
			name:      "encrypted image without legacy body",
			content:   `{"msgtype":"m.image","file":{"url":"mxc://example.org/bodyless-image","key":{},"iv":"iv","hashes":{},"v":"v2"},"info":{"mimetype":"image/jpeg"}}`,
			wantURI:   "mxc://example.org/bodyless-image",
			wantMIME:  "image/jpeg",
			wantBody:  "Image",
			encrypted: true,
		},
		{
			name:     "extensible text without legacy body",
			content:  `{"m.text":[{"body":"hello from extensible text"}]}`,
			wantBody: "hello from extensible text",
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
			if message.MediaMIME != test.wantMIME {
				t.Errorf("media MIME = %q, want %q", message.MediaMIME, test.wantMIME)
			}
			if test.wantBody != "" && message.Body != test.wantBody {
				t.Errorf("message body = %q, want %q", message.Body, test.wantBody)
			}
			if message.MediaWidth != test.wantWidth || message.MediaHeight != test.wantHeight {
				t.Errorf("media dimensions = %dx%d, want %dx%d", message.MediaWidth, message.MediaHeight, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func TestMessageFromEventIncludesEditRedactionAndDecryptionStatus(t *testing.T) {
	editedRow := database.EventRowID(2)
	tests := []struct {
		name           string
		event          *database.Event
		wantBody       string
		wantEdited     bool
		wantRedacted   bool
		wantDecryption string
	}{
		{
			name:       "edited",
			event:      &database.Event{ID: "$edited", Type: event.EventMessage.Type, Content: json.RawMessage(`{"body":"updated"}`), LastEditRowID: &editedRow},
			wantBody:   "updated",
			wantEdited: true,
		},
		{
			name:         "redacted",
			event:        &database.Event{ID: "$redacted", Type: event.EventMessage.Type, RedactedBy: "$redaction"},
			wantEdited:   false,
			wantRedacted: true,
		},
		{
			name:           "decryption failure",
			event:          &database.Event{ID: "$encrypted", Type: event.EventEncrypted.Type, DecryptionError: "missing room key"},
			wantDecryption: "missing room key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, ok := messageFromEvent(test.event)
			if !ok {
				t.Fatal("expected event to be represented in the timeline")
			}
			if message.Body != test.wantBody || message.Edited != test.wantEdited ||
				message.Redacted != test.wantRedacted || message.DecryptionError != test.wantDecryption {
				t.Fatalf("unexpected event status: %+v", message)
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
			name: "redacted reaction",
			evt:  &database.Event{Type: event.EventReaction.Type, RedactedBy: "$redaction"},
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
	if roomNeedsReply(room, "@me:example.org", "") {
		t.Fatal("replying should clear needs-reply even while the stale unread count remains")
	}
	room.LastEventSender = "@alice:example.org"
	if !roomNeedsReply(room, "@me:example.org", "") {
		t.Fatal("unread messages from someone else should put the room in needs-reply")
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

func TestApplySyncKeepsPreviewAndTimelineInSync(t *testing.T) {
	client := &Client{
		events: make(chan Update, 1),
		done:   make(chan struct{}),
		rooms:  map[string]Room{},
	}
	roomID := id.RoomID("!chat:example.org")
	latest := time.Date(2026, time.October, 17, 11, 57, 0, 0, time.UTC)
	stale := latest.Add(-time.Minute)
	client.rooms[roomID.String()] = Room{ID: roomID.String(), Name: "Andrea", Preview: "older", LastSeen: stale}
	syncEvent := &jsoncmd.SyncComplete{
		Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
			roomID: {
				Timeline: []database.TimelineRowTuple{{Timeline: 2, Event: 2}},
				Events: []*database.Event{
					{
						RowID:     2,
						Type:      event.EventMessage.Type,
						Content:   json.RawMessage(`{"body":"se hai bisogno dimmi"}`),
						Timestamp: jsontime.UM(latest),
					},
				},
			},
		},
	}
	client.applySync(syncEvent, true)
	rooms := client.Rooms()
	if len(rooms) != 1 || rooms[0].Preview != "se hai bisogno dimmi" || !rooms[0].LastSeen.Equal(latest) {
		t.Fatalf("preview not synced with timeline: %+v", rooms)
	}
	update := <-client.Events()
	if len(update.TimelineChangedRoomID) != 1 || update.TimelineChangedRoomID[0] != roomID.String() {
		t.Fatalf("timeline changed rooms = %v, want open chat refresh", update.TimelineChangedRoomID)
	}
	// Non-timeline events must not move the preview or trigger a refresh.
	client.applySync(&jsoncmd.SyncComplete{
		Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
			roomID: {
				Events: []*database.Event{
					{
						RowID:     99,
						Type:      event.EventMessage.Type,
						Content:   json.RawMessage(`{\"body\":\"background noise\"}`),
						Timestamp: jsontime.UM(latest.Add(time.Minute)),
					},
				},
			},
		},
	}, true)
	rooms = client.Rooms()
	if len(rooms) != 1 || rooms[0].Preview != "se hai bisogno dimmi" {
		t.Fatalf("non-timeline event moved preview: %+v", rooms)
	}
	update = <-client.Events()
	if len(update.TimelineChangedRoomID) != 0 {
		t.Fatalf("timeline changed rooms = %v, want no refresh", update.TimelineChangedRoomID)
	}
}

// gomuks picks a room's preview event server-side (room.preview_event_rowid).
// When that event's body arrives in this payload the sidebar must use it:
// re-deriving the preview from the newest in-timeline event can surface the
// bridge placeholder text instead of the message the open chat shows.
func TestApplySyncPrefersAuthoritativePreviewEvent(t *testing.T) {
	client := &Client{
		events: make(chan Update, 1),
		done:   make(chan struct{}),
		rooms:  map[string]Room{},
	}
	roomID := id.RoomID("!bridge:example.org")
	displayName := "WhatsApp chat"
	topic := "WhatsApp private chat"
	sent := time.Date(2026, time.October, 17, 12, 59, 0, 0, time.UTC)
	client.rooms[roomID.String()] = Room{
		ID: roomID.String(), Name: displayName, Topic: topic,
		LastSeen: sent.Add(-time.Minute),
	}
	client.applySync(&jsoncmd.SyncComplete{
		Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
			roomID: {
				Meta: &database.Room{
					ID:                roomID,
					Name:              &displayName,
					Topic:             &topic,
					PreviewEventRowID: 5,
					SortingTimestamp:  jsontime.UM(sent),
				},
				Timeline: []database.TimelineRowTuple{{Timeline: 1, Event: 9}},
				Events: []*database.Event{
					{
						RowID:     9,
						Type:      event.EventMessage.Type,
						Content:   json.RawMessage(`{"body":"WhatsApp private chat"}`),
						Timestamp: jsontime.UM(sent),
					},
					{
						RowID:     5,
						Type:      event.EventMessage.Type,
						Content:   json.RawMessage(`{"body":"i want a yes"}`),
						Timestamp: jsontime.UM(sent),
					},
				},
			},
		},
	}, false)
	rooms := client.Rooms()
	if len(rooms) != 1 || rooms[0].Preview != "i want a yes" {
		t.Fatalf("authoritative preview not used: %+v", rooms)
	}
}

// A freshly sent own message can arrive in `events` before it has a timeline
// row (the timeline insert may land a sync later). It must still seed an
// otherwise-empty preview, otherwise the sidebar falls back to the room topic
// while the open chat already shows the sent text.
func TestApplySyncSeedsEmptyPreviewFromOwnPendingSend(t *testing.T) {
	client := &Client{
		events: make(chan Update, 1),
		done:   make(chan struct{}),
		rooms:  map[string]Room{},
	}
	roomID := id.RoomID("!bridge:example.org")
	topic := "WhatsApp private chat"
	sent := time.Date(2026, time.October, 17, 12, 59, 0, 0, time.UTC)
	client.rooms[roomID.String()] = Room{
		ID: roomID.String(), Name: "WhatsApp chat", Topic: topic,
		LastSeen: sent.Add(-time.Minute),
	}
	client.applySync(&jsoncmd.SyncComplete{
		Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
			roomID: {
				// No timeline rows: the send has not been persisted yet.
				Events: []*database.Event{
					{
						RowID:     7,
						Type:      event.EventMessage.Type,
						Content:   json.RawMessage(`{"body":"i want a yes"}`),
						Timestamp: jsontime.UM(sent),
					},
				},
			},
		},
	}, false)
	rooms := client.Rooms()
	if len(rooms) != 1 || rooms[0].Preview != "i want a yes" {
		t.Fatalf("pending own send did not seed preview: %+v", rooms)
	}
	if !rooms[0].LastSeen.Equal(sent) {
		t.Fatalf("last seen = %v, want %v", rooms[0].LastSeen, sent)
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
				Meta:     room,
				Timeline: []database.TimelineRowTuple{{Timeline: 7, Event: 7}},
				Events: []*database.Event{
					{
						RowID:     7,
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

func TestApplySyncUsesDirectRoomMemberAvatar(t *testing.T) {
	client, err := New(filepath.Join(t.TempDir(), "profile", "matrix.db"))
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	roomID := id.RoomID("!dm:example.org")
	userID := id.UserID("@alice:example.org")
	avatar := id.ContentURIString("mxc://example.org/alice-avatar")
	stateKey := userID.String()
	if err = client.engine.DB.Room.CreateRow(ctx, roomID); err != nil {
		t.Fatalf("create room row: %v", err)
	}
	memberEvent := &database.Event{
		RoomID:    roomID,
		ID:        id.EventID("$alice-member"),
		Sender:    userID,
		Type:      event.StateMember.Type,
		StateKey:  &stateKey,
		Timestamp: jsontime.UM(time.Now()),
		Content:   json.RawMessage(`{"displayname":"Alice","avatar_url":"` + string(avatar) + `"}`),
		Unsigned:  json.RawMessage(`{}`),
	}
	rowID, err := client.engine.DB.Event.Insert(ctx, memberEvent)
	if err != nil {
		t.Fatalf("insert member state: %v", err)
	}
	if err = client.engine.DB.CurrentState.Set(ctx, roomID, event.StateMember, userID.String(), rowID, ""); err != nil {
		t.Fatalf("set current member state: %v", err)
	}

	displayName := "Alice"
	client.applySync(&jsoncmd.SyncComplete{
		Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
			roomID: {Meta: &database.Room{ID: roomID, Name: &displayName, DMUserID: &userID}},
		},
	}, false)

	rooms := client.Rooms()
	if len(rooms) != 1 || rooms[0].Avatar != string(avatar) {
		t.Fatalf("direct room avatar = %+v, want %q", rooms, avatar)
	}
}

func TestApplySyncClassifiesUnmarkedDirectRoomsByMemberCount(t *testing.T) {
	tests := []struct {
		name       string
		joined     int
		invited    int
		wantDirect bool
	}{
		{name: "two joined members", joined: 2, wantDirect: true},
		{name: "joined and invited members", joined: 1, invited: 1, wantDirect: true},
		{name: "three joined members", joined: 3, wantDirect: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roomID := id.RoomID("!" + strings.ReplaceAll(test.name, " ", "-") + ":example.org")
			joined, invited := test.joined, test.invited
			client := &Client{rooms: make(map[string]Room)}
			client.applySync(&jsoncmd.SyncComplete{
				Rooms: map[id.RoomID]*jsoncmd.SyncRoom{
					roomID: {Meta: &database.Room{
						ID: roomID,
						LazyLoadSummary: &mautrix.LazyLoadSummary{
							JoinedMemberCount:  &joined,
							InvitedMemberCount: &invited,
						},
					}},
				},
			}, false)

			rooms := client.Rooms()
			if len(rooms) != 1 || rooms[0].Direct != test.wantDirect {
				t.Fatalf("direct = %v, want %v; rooms = %+v", len(rooms) == 1 && rooms[0].Direct, test.wantDirect, rooms)
			}
		})
	}
}

func TestMissingMatrixMediaErrorIsRecognized(t *testing.T) {
	if !isMissingMatrixMediaError(fmt.Errorf("download Matrix media: %w", mautrix.MNotFound)) {
		t.Fatal("wrapped M_NOT_FOUND error was not recognized")
	}
	if isMissingMatrixMediaError(fmt.Errorf("media server unavailable")) {
		t.Fatal("non-404 error was treated as missing media")
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

func TestWaitForCacheWaitsUntilRestoreCompletes(t *testing.T) {
	client := &Client{cacheReady: make(chan struct{})}
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		result <- client.WaitForCache(context.Background())
	}()
	<-started
	select {
	case err := <-result:
		t.Fatalf("WaitForCache returned before restore completed: %v", err)
	default:
	}

	close(client.cacheReady)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("WaitForCache: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("WaitForCache did not return after restore completed")
	}
}

func TestLocalSearchParamsSupportsGlobalFiltersAndPagination(t *testing.T) {
	minTimestamp := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	maxTimestamp := time.Date(2026, time.October, 17, 23, 59, 59, 999000000, time.UTC)
	request := SearchRequest{
		Query:        "  project update  ",
		RoomID:       "!room:example.org",
		Sender:       "@alice:example.org",
		MinTimestamp: minTimestamp.UnixMilli(),
		MaxTimestamp: maxTimestamp.UnixMilli(),
		Limit:        25,
		NextBatch:    "local_offset:25",
	}

	params, err := localSearchParams(request)
	if err != nil {
		t.Fatalf("build search params: %v", err)
	}
	if params.SearchTerm != "project update" || !params.SortByTime || params.Limit != 25 || params.NextBatch != request.NextBatch {
		t.Fatalf("unexpected search params: %+v", params)
	}
	if len(params.RoomIDs) != 1 || params.RoomIDs[0] != id.RoomID(request.RoomID) {
		t.Fatalf("room filter = %v, want %q", params.RoomIDs, request.RoomID)
	}
	if len(params.Senders) != 1 || params.Senders[0] != id.UserID(request.Sender) {
		t.Fatalf("sender filter = %v, want %q", params.Senders, request.Sender)
	}
	if params.MinTimestamp.UnixMilli() != request.MinTimestamp || params.MaxTimestamp.UnixMilli() != request.MaxTimestamp {
		t.Fatalf("timestamp filters = (%d, %d), want (%d, %d)", params.MinTimestamp.UnixMilli(), params.MaxTimestamp.UnixMilli(), request.MinTimestamp, request.MaxTimestamp)
	}

	globalParams, err := localSearchParams(SearchRequest{Query: "topic"})
	if err != nil {
		t.Fatalf("build global search params: %v", err)
	}
	if len(globalParams.RoomIDs) != 0 || globalParams.Limit != 50 {
		t.Fatalf("global search params = %+v, want no room filter and default limit 50", globalParams)
	}
}

func TestReconnectDelayDoublesAndCaps(t *testing.T) {
	got := []time.Duration{
		LastReconnectDelay(0),
		LastReconnectDelay(1),
		LastReconnectDelay(2),
		LastReconnectDelay(5),
		LastReconnectDelay(6),
		LastReconnectDelay(20),
	}
	want := []time.Duration{time.Second, time.Second, 2 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reconnect delay[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestParseMXCRejectsInvalidURIs(t *testing.T) {
	server, mediaID, err := parseMXC("mxc://example.org/media123")
	if err != nil || server != "example.org" || mediaID != "media123" {
		t.Fatalf("parse mxc = (%q, %q, %v)", server, mediaID, err)
	}
	for _, invalid := range []string{"", "https://example.org/x", "mxc://", "mxc://server/", "mxc:///id", "mxc://s/a/b"} {
		if _, _, err = parseMXC(invalid); err == nil {
			t.Fatalf("expected error for %q", invalid)
		}
	}
}

func TestMediaCacheUsageAndClear(t *testing.T) {
	client := &Client{mediaCacheDir: filepath.Join(t.TempDir(), "media")}
	if err := os.MkdirAll(client.MediaCacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client.MediaCacheDir(), "a"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client.MediaCacheDir(), "b"), []byte("world!"), 0o600); err != nil {
		t.Fatal(err)
	}
	bytes, files, err := client.MediaCacheUsage()
	if err != nil || files != 2 || bytes != 11 {
		t.Fatalf("media usage = (%d, %d, %v)", bytes, files, err)
	}
	if path, err := client.mediaCachePath("mxc://example.org/media123"); err != nil || !strings.HasPrefix(path, client.MediaCacheDir()) {
		t.Fatalf("media cache path = %q, %v", path, err)
	}
	if err := client.ClearMediaCache(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.MediaCacheUsage(); err != nil {
		t.Fatal(err)
	}
}

func TestCachedMediaBytesUsesIncomingEncryptionMetadata(t *testing.T) {
	uri := "mxc://example.org/encrypted-image"
	client := &Client{mediaCacheDir: t.TempDir()}
	client.rememberMediaEncryption([]Message{{
		MediaURI:       uri,
		MediaEncrypted: &event.EncryptedFileInfo{},
	}})

	plainPath, err := client.mediaCachePathFor(uri, false)
	if err != nil {
		t.Fatal(err)
	}
	encryptedPath, err := client.mediaCachePathFor(uri, true)
	if err != nil {
		t.Fatal(err)
	}
	if plainPath == encryptedPath {
		t.Fatal("encrypted and plaintext media must use distinct cache paths")
	}
	if err := os.MkdirAll(filepath.Dir(encryptedPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plainPath, []byte("ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(encryptedPath, []byte("decrypted image"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := client.CachedMediaBytes(context.Background(), uri)
	if err != nil || string(got) != "decrypted image" {
		t.Fatalf("cached media = %q, %v; want decrypted image", got, err)
	}
	if path, ok := client.CachedMediaPath(uri); !ok || path != encryptedPath {
		t.Fatalf("cached media path = %q, %v; want %q", path, ok, encryptedPath)
	}
}

func TestBridgeLabelDetectsNetworks(t *testing.T) {
	name := "WhatsApp Bridge"
	client := &Client{}
	_ = client
	room := roomForBridgeTest(&name, "@_whatsapp_user:example.org")
	if got := bridgeLabel(room); got != "WA" {
		t.Fatalf("bridge = %q, want WA", got)
	}
	plain := roomForBridgeTest(nil, "")
	if got := bridgeLabel(plain); got != "" {
		t.Fatalf("plain bridge = %q, want empty", got)
	}
	if got := bridgeLabel(nil); got != "" {
		t.Fatalf("nil bridge = %q, want empty", got)
	}
}

func roomForBridgeTest(name *string, dmUser string) *database.Room {
	room := &database.Room{ID: "!room:example.org", Name: name}
	if dmUser != "" {
		dm := id.UserID(dmUser)
		room.DMUserID = &dm
	}
	return room
}

func stringPointer(value string) *string {
	return &value
}

func TestMessageFromEventTreatsNotSentAsPendingNotError(t *testing.T) {
	evt := &database.Event{
		ID:        id.EventID("~hicli-1"),
		Sender:    id.UserID("@me:example.org"),
		Type:      event.EventMessage.Type,
		Content:   json.RawMessage(`{"msgtype":"m.text","body":"hi"}`),
		SendError: "not sent",
	}
	message, ok := messageFromEvent(evt)
	if !ok {
		t.Fatal("expected pending message to be represented")
	}
	if !message.Sending {
		t.Fatalf("Sending = false, want true: %+v", message)
	}
	if message.SendError != "" {
		t.Fatalf("SendError = %q, want empty (pending marker is not a failure)", message.SendError)
	}
	if message.Edited {
		t.Fatalf("fresh send flagged as edited: %+v", message)
	}
}

func TestMessageFromEventKeepsRealSendErrors(t *testing.T) {
	evt := &database.Event{
		ID:        id.EventID("~hicli-2"),
		Sender:    id.UserID("@me:example.org"),
		Type:      event.EventMessage.Type,
		Content:   json.RawMessage(`{"msgtype":"m.text","body":"hi"}`),
		SendError: "failed to encrypt: boom",
	}
	message, ok := messageFromEvent(evt)
	if !ok {
		t.Fatal("expected failed message to be represented")
	}
	if message.SendError != "failed to encrypt: boom" {
		t.Fatalf("SendError = %q, want the real failure", message.SendError)
	}
}

func TestMessageFromEventClearsSendingForConfirmedEvent(t *testing.T) {
	// gomuks leaves the "not sent" marker on the row even after a successful
	// send (its deferred cleanup re-writes it once UpdateID has cleared it), so
	// a real event ID must win over send_error and drop the pending state.
	evt := &database.Event{
		ID:        id.EventID("$confirmed:example.org"),
		Sender:    id.UserID("@me:example.org"),
		Type:      event.EventMessage.Type,
		Content:   json.RawMessage(`{"msgtype":"m.text","body":"hi"}`),
		SendError: "not sent",
	}
	message, ok := messageFromEvent(evt)
	if !ok {
		t.Fatal("expected confirmed message to be represented")
	}
	if message.Sending {
		t.Fatalf("Sending = true for a confirmed event, want false: %+v", message)
	}
	if message.SendError != "" {
		t.Fatalf("SendError = %q, want empty for a confirmed event", message.SendError)
	}
}

func TestMessageFromEventKeepsUnplacedEventPending(t *testing.T) {
	evt := &database.Event{
		Sender:    id.UserID("@me:example.org"),
		Type:      event.EventMessage.Type,
		Content:   json.RawMessage(`{"msgtype":"m.text","body":"hi"}`),
		SendError: "not sent",
	}
	message, ok := messageFromEvent(evt)
	if !ok {
		t.Fatal("expected unplaced message to be represented")
	}
	if !message.Sending {
		t.Fatalf("Sending = false for an event without an ID, want true: %+v", message)
	}
}

func TestShadowedByServerRowCoversRenamedOptimisticEntry(t *testing.T) {
	sentAt := time.Now()
	server := []Message{{ID: "$real", Sender: "@me:example.org", Body: "hi", Timestamp: sentAt}}
	pending := Message{ID: "~hicli-1", Sender: "@me:example.org", Body: "hi", Sending: true, Timestamp: sentAt}
	if !shadowedByServerRow(server, pending) {
		t.Fatal("expected server-confirmed row to shadow the stale pending entry")
	}
	other := Message{ID: "~hicli-9", Sender: "@me:example.org", Body: "different", Sending: true, Timestamp: sentAt}
	if shadowedByServerRow(server, other) {
		t.Fatal("different body must not be shadowed")
	}
}
