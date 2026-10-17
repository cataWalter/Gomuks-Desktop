package matrix

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"maunium.net/go/mautrix/event"
)

func writeCacheFile(t *testing.T, client *Client, mxc string, encrypted bool, data string) string {
	t.Helper()
	path, err := client.mediaCachePathFor(mxc, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMediaCategoryMapsMessageTypes(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		string(event.MsgImage): MediaCategoryImage,
		string(event.MsgVideo): MediaCategoryVideo,
		string(event.MsgAudio): MediaCategoryAudio,
		string(event.MsgFile):  MediaCategoryFile,
		"m.beeper.gallery":     MediaCategoryOther,
		"m.sticker":            MediaCategoryOther,
	}
	for input, want := range cases {
		if got := mediaCategory(input); got != want {
			t.Fatalf("mediaCategory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMediaCleanupFilterValidateAndMatch(t *testing.T) {
	if err := (MediaCleanupFilter{MediaType: "gif"}).Validate(); err == nil {
		t.Fatal("expected unknown media type to be rejected")
	}
	if err := (MediaCleanupFilter{MediaType: MediaCategoryImage}).Validate(); err != nil {
		t.Fatalf("image filter rejected: %v", err)
	}

	cutoff := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	old := Message{Timestamp: cutoff.Add(-time.Hour), MediaType: string(event.MsgImage)}
	recent := Message{Timestamp: cutoff.Add(time.Hour), MediaType: string(event.MsgImage)}
	video := Message{Timestamp: cutoff.Add(-time.Hour), MediaType: string(event.MsgVideo)}

	filter := MediaCleanupFilter{OlderThan: cutoff, MediaType: MediaCategoryImage}
	if !filter.Matches(old) {
		t.Fatal("old image should match")
	}
	if filter.Matches(recent) {
		t.Fatal("recent image should not match an older-than filter")
	}
	if filter.Matches(video) {
		t.Fatal("video should not match an image filter")
	}
	if !(MediaCleanupFilter{}).Matches(video) {
		t.Fatal("zero filter should match everything")
	}
}

func TestSummarizeRoomStorageCountsExistingMediaOnce(t *testing.T) {
	client := &Client{mediaCacheDir: t.TempDir()}
	const uri = "mxc://example.org/photo"
	writeCacheFile(t, client, uri, false, "pixels")

	messages := []Message{
		{Body: "hello", FormattedBody: "<b>hello</b>", MediaURI: uri, MediaType: string(event.MsgImage)},
		{Body: "again", MediaURI: uri, MediaType: string(event.MsgImage)},
		{Body: "missing", MediaURI: "mxc://example.org/gone", MediaType: string(event.MsgImage)},
	}
	summary := client.summarizeRoomStorage("!room:example.org", "Room", messages)
	if summary.Messages != 3 {
		t.Fatalf("messages = %d, want 3", summary.Messages)
	}
	wantText := int64(len("hello") + len("<b>hello</b>") + len("again") + len("missing"))
	if summary.TextBytes != wantText {
		t.Fatalf("text bytes = %d, want %d", summary.TextBytes, wantText)
	}
	if summary.MediaFiles != 1 || summary.MediaBytes != int64(len("pixels")) {
		t.Fatalf("media = (%d files, %d bytes), want (1, 6)", summary.MediaFiles, summary.MediaBytes)
	}
}

func TestRemoveCachedMediaDeletesOnlyMatchingFiles(t *testing.T) {
	client := &Client{mediaCacheDir: t.TempDir()}
	cutoff := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	oldImage := writeCacheFile(t, client, "mxc://example.org/old-image", false, "aaaa")
	recentImage := writeCacheFile(t, client, "mxc://example.org/new-image", false, "bb")
	oldVideo := writeCacheFile(t, client, "mxc://example.org/old-video", true, "cccccc")

	messages := []Message{
		{MediaURI: "mxc://example.org/old-image", MediaType: string(event.MsgImage), Timestamp: cutoff.Add(-time.Hour)},
		{MediaURI: "mxc://example.org/new-image", MediaType: string(event.MsgImage), Timestamp: cutoff.Add(time.Hour)},
		{MediaURI: "mxc://example.org/old-video", MediaEncrypted: &event.EncryptedFileInfo{}, MediaType: string(event.MsgVideo), Timestamp: cutoff.Add(-time.Hour)},
	}

	result, err := client.removeCachedMedia(messages, MediaCleanupFilter{OlderThan: cutoff, MediaType: MediaCategoryImage}, map[string]struct{}{})
	if err != nil {
		t.Fatalf("remove cached media: %v", err)
	}
	if result.Files != 1 || result.Bytes != 4 {
		t.Fatalf("result = %+v, want 1 file and 4 bytes", result)
	}
	if _, err := os.Stat(oldImage); !os.IsNotExist(err) {
		t.Fatalf("old image should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(recentImage); err != nil {
		t.Fatalf("recent image should be kept: %v", err)
	}
	if _, err := os.Stat(oldVideo); err != nil {
		t.Fatalf("video outside the filter should be kept: %v", err)
	}
}

func TestCachedMediaFilesRequireCacheDirectory(t *testing.T) {
	client := &Client{}
	files := client.cachedMediaFilesFor(Message{MediaURI: "mxc://example.org/x", MediaType: string(event.MsgImage)})
	if len(files) != 0 {
		t.Fatalf("expected no files without a cache directory, got %+v", files)
	}
}
