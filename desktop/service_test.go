package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mychat/matrix"
)

func TestMapRoomsWithPreferences(t *testing.T) {
	rooms := []matrix.Room{{ID: "!room:example.org", Name: "General"}}
	preferences := map[string]any{
		"room.favorite.!room:example.org": true,
		"room.muted.!room:example.org":    true,
	}

	mapped := mapRoomsWithPreferences(rooms, preferences)
	if len(mapped) != 1 {
		t.Fatalf("mapped room count = %d, want 1", len(mapped))
	}
	if !mapped[0].Favorite || !mapped[0].Muted {
		t.Fatalf("room preferences were not mapped: %+v", mapped[0])
	}
	if rooms[0].Favorite || rooms[0].Muted {
		t.Fatal("mapping preferences mutated the Matrix client room snapshot")
	}
}

func TestReadDroppedAttachment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared image.png")
	want := []byte("image data")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	filename, data, err := readDroppedAttachment(path)
	if err != nil {
		t.Fatal(err)
	}
	if filename != "shared image.png" {
		t.Fatalf("filename = %q, want %q", filename, "shared image.png")
	}
	if string(data) != string(want) {
		t.Fatalf("data = %q, want %q", data, want)
	}
}

func TestReadDroppedAttachmentRejectsOversizedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxAttachmentSize + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = readDroppedAttachment(path)
	if err == nil || !strings.Contains(err.Error(), "50 MiB") {
		t.Fatalf("error = %v, want 50 MiB limit error", err)
	}
}

func TestReadDroppedAttachmentRejectsDirectories(t *testing.T) {
	_, _, err := readDroppedAttachment(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("error = %v, want regular file error", err)
	}
}

func TestSendAttachmentRejectsOversizedData(t *testing.T) {
	service := &AppService{}
	err := service.SendAttachment("!room:example.org", "large.bin", make([]byte, maxAttachmentSize+1))
	if err == nil || !strings.Contains(err.Error(), "50 MiB") {
		t.Fatalf("error = %v, want 50 MiB limit error", err)
	}
}
