package matrix

import (
	"encoding/json"
	"testing"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix/event"
)

func TestVideoMessageExposesThumbnail(t *testing.T) {
	evt := &database.Event{
		Type:    event.EventMessage.Type,
		Content: json.RawMessage(`{"msgtype":"m.video","body":"clip.mp4","url":"mxc://example.org/video","info":{"mimetype":"video/mp4","duration":65000,"thumbnail_url":"mxc://example.org/thumb","thumbnail_info":{"mimetype":"image/jpeg"}}}`),
	}
	message, ok := messageFromEvent(evt)
	if !ok {
		t.Fatal("video message was not mapped")
	}
	if message.MediaType != "m.video" || message.MediaURI != "mxc://example.org/video" || message.MediaThumbnailURI != "mxc://example.org/thumb" {
		t.Fatalf("type = %q, media = %q, thumbnail = %q", message.MediaType, message.MediaURI, message.MediaThumbnailURI)
	}
}
