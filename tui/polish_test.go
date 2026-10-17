package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mychat/matrix"
)

func TestLongMessageWrapsWithinViewportWidth(t *testing.T) {
	timeline := newTimelineModel()
	timeline.viewport.Width = 40
	timeline.setMessages([]matrix.Message{{ID: "$long", SenderName: "Alice", Body: strings.Repeat("lorem ipsum ", 20)}}, false)
	rows := strings.Split(timeline.renderMessages(), "\n")
	if len(rows) < 3 {
		t.Fatalf("long message rendered in %d rows, want wrapped rows", len(rows))
	}
	for _, row := range rows {
		if width := lipgloss.Width(row); width > 40 {
			t.Fatalf("row width = %d, want at most 40: %q", width, row)
		}
	}
}

func TestSelectionFollowsNewestMessageOnlyWhenAtBottom(t *testing.T) {
	timeline := newTimelineModel()
	timeline.setMessages([]matrix.Message{{ID: "$a"}, {ID: "$b"}}, false)
	timeline.setMessages([]matrix.Message{{ID: "$a"}, {ID: "$b"}, {ID: "$c"}}, false)
	if selected := timeline.selectedMessage(); selected == nil || selected.ID != "$c" {
		t.Fatalf("selected = %+v, want newest $c", selected)
	}
	timeline.moveSelection(-10)
	timeline.setMessages([]matrix.Message{{ID: "$a"}, {ID: "$b"}, {ID: "$c"}, {ID: "$d"}}, false)
	if selected := timeline.selectedMessage(); selected == nil || selected.ID != "$a" {
		t.Fatalf("selected = %+v, want $a kept while reading history", selected)
	}
}

func TestEscapeCancelsComposerModeThenReturnsToTimeline(t *testing.T) {
	model := mediaTestModel()
	model.setFocus(FocusComposer)
	model.composer.mode = ModeReply
	model.composer.replyTo = &matrix.Message{ID: "$x", SenderName: "Bob"}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.composer.mode != ModeNormal || model.focus != FocusComposer {
		t.Fatalf("mode = %v, focus = %v; want normal composer", model.composer.mode, model.focus)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).focus != FocusTimeline {
		t.Fatal("second escape did not return focus to the timeline")
	}
}

func TestAltEnterInsertsNewlineInComposer(t *testing.T) {
	model := mediaTestModel()
	model.setFocus(FocusComposer)
	model.composer.textarea.SetValue("first")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := updated.(Model).composer.textarea.Value(); !strings.Contains(got, "\n") {
		t.Fatalf("composer text = %q, want a newline", got)
	}
}

func TestComposerHintNamesReplyTarget(t *testing.T) {
	model := mediaTestModel()
	model.setFocus(FocusComposer)
	model.composer.mode = ModeReply
	model.composer.replyTo = &matrix.Message{ID: "$x", SenderName: "Bob"}
	if text := model.statusText(); !strings.Contains(text, "replying to Bob") {
		t.Fatalf("status = %q, want the reply target", text)
	}
}

func TestPrependedHistoryKeepsSelectionAndSkipsDuplicates(t *testing.T) {
	timeline := newTimelineModel()
	newer := make([]matrix.Message, 0, 20)
	for i := range 20 {
		newer = append(newer, matrix.Message{ID: fmt.Sprintf("$n%02d", i), Body: "newer"})
	}
	timeline.setMessages(newer, true)
	older := make([]matrix.Message, 0, 11)
	for i := range 10 {
		older = append(older, matrix.Message{ID: fmt.Sprintf("$o%02d", i), Body: "older"})
	}
	older = append(older, matrix.Message{ID: "$n00", Body: "newer"})
	timeline.prependMessages(older, false)
	if got := len(timeline.messages); got != 30 {
		t.Fatalf("messages = %d, want 30 without the duplicate", got)
	}
	if selected := timeline.selectedMessage(); selected == nil || selected.ID != "$n19" {
		t.Fatalf("selected = %+v, want $n19", selected)
	}
	if timeline.hasMore {
		t.Fatal("hasMore stayed true after the oldest page")
	}
}

func TestOlderHistoryNeedsServerClient(t *testing.T) {
	model := mediaTestModel()
	if cmd := model.loadOlderCmd(); cmd != nil {
		t.Fatal("loadOlderCmd returned a command without a client")
	}
}
