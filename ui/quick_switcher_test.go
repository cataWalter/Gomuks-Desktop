package ui

import (
	"testing"

	"mychat/matrix"
)

func TestFuzzyScoreMatchesSubsequencesAndPrefersContiguousText(t *testing.T) {
	subsequenceScore, ok := fuzzyScore("ac", "Alice Example")
	if !ok {
		t.Fatal("expected a subsequence match")
	}
	contiguousScore, ok := fuzzyScore("ali", "Alice Example")
	if !ok {
		t.Fatal("expected a contiguous match")
	}
	if contiguousScore <= subsequenceScore {
		t.Fatalf("contiguous score %d should exceed subsequence score %d", contiguousScore, subsequenceScore)
	}
	if _, ok = fuzzyScore("zz", "Alice Example"); ok {
		t.Fatal("unexpected match for absent characters")
	}
}

func TestReplyQuoteResolvesPreviewAndScrollTarget(t *testing.T) {
	view := NewChatView(nil)
	view.SetMessages([]matrix.Message{
		{ID: "$original", Sender: "@alice:example.org", SenderName: "Alice", Body: "original body"},
		{ID: "$reply", Sender: "@bob:example.org", Body: "reply body", ReplyToEventID: "$original"},
	})
	view.SetHistoryPage(matrix.HistoryPage{Messages: view.history}, false)
	quote := view.ReplyPreviewFor(view.history[1])
	if quote != "↩ Alice: original body" {
		t.Fatalf("reply quote = %q", quote)
	}
}

func TestFormatReactionsHighlightsOwnReaction(t *testing.T) {
	got := formatReactions(map[string]int{"👍": 2, "❤️": 1}, map[string]bool{"👍": true})
	if got != "[❤️ 1] [● 👍 2]" {
		t.Fatalf("reactions = %q", got)
	}
	if got := formatReactions(nil, nil); got != "" {
		t.Fatalf("empty reactions = %q", got)
	}
}

func TestExtractCodeBlock(t *testing.T) {
	body := "intro\n```go\nfmt.Println()\n```\ntail"
	if got := extractCodeBlock(body); got != "fmt.Println()" {
		t.Fatalf("code block = %q", got)
	}
	if got := extractCodeBlock("no fences"); got != "" {
		t.Fatalf("plain code block = %q", got)
	}
}

func TestFileKindAndDurationHelpers(t *testing.T) {
	if got := fileKindLabel("audio/ogg"); got != "Audio" {
		t.Fatalf("kind = %q", got)
	}
	if got := formatDuration(95000); got != "1:35" {
		t.Fatalf("duration = %q", got)
	}
}
