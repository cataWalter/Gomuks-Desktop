package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mychat/matrix"
)

type ChatView struct {
	title     *widget.Label
	topic     *widget.Label
	status    *widget.Label
	empty     *widget.Label
	loadOlder *widget.Button
	messages  *widget.List
	input     *widget.Entry
	send      *widget.Button

	room            matrix.Room
	history         []matrix.Message
	onSend          func(roomID, text string)
	onLoadOlder     func(roomID string, before int64)
	before          int64
	hasOlder        bool
	loadingOlder    bool
	loadedOlder     bool
	loadingMessages bool
}

func NewChatView(onSend func(roomID, text string)) *ChatView {
	view := &ChatView{
		title:  widget.NewLabelWithStyle("Select a room", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		topic:  widget.NewLabel(""),
		status: widget.NewLabel(""),
		empty:  widget.NewLabel("Select a chat to see its messages."),
		input:  widget.NewEntry(),
		onSend: onSend,
	}
	view.empty.Wrapping = fyne.TextWrapWord
	view.loadOlder = widget.NewButton("Load older", view.requestOlder)
	view.loadOlder.Disable()
	view.input.SetPlaceHolder("Write a message...")
	view.messages = widget.NewList(
		func() int { return len(view.history) },
		func() fyne.CanvasObject {
			return container.NewVBox(widget.NewLabel(""), widget.NewLabel(""))
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(view.history) {
				return
			}
			message := view.history[id]
			box := object.(*fyne.Container)
			details := fmt.Sprintf("%s | %s",
				message.Sender, message.Timestamp.Local().Format(time.Kitchen))
			if message.Sending {
				details += " | Sending..."
			} else if message.SendError != "" {
				details += " | Failed to send: " + message.SendError
			}
			box.Objects[0].(*widget.Label).SetText(details)
			box.Objects[1].(*widget.Label).SetText(message.Body)
		},
	)
	view.send = widget.NewButtonWithIcon("", theme.MailSendIcon(), view.submit)
	view.input.OnSubmitted = func(string) { view.submit() }
	view.setComposerEnabled(false)
	return view
}

func (v *ChatView) Object() fyne.CanvasObject {
	titleBar := container.NewBorder(nil, nil, nil, v.loadOlder, v.title)
	header := container.NewVBox(titleBar, v.topic, v.status, widget.NewSeparator())
	bottom := container.NewBorder(nil, nil, nil, v.send, v.input)
	return container.NewBorder(header, bottom, nil, nil, container.NewStack(v.messages, container.NewCenter(v.empty)))
}

func (v *ChatView) SetRoom(room matrix.Room) {
	v.UpdateRoom(room)
	v.before = 0
	v.hasOlder = false
	v.loadingOlder = false
	v.loadedOlder = false
	v.SetHistoryPage(matrix.HistoryPage{}, false)
	v.SetMessagesLoading(room.ID != "")
	v.updateEmptyState()
	v.updateOlderButton()
}

func (v *ChatView) UpdateRoom(room matrix.Room) {
	v.room = room
	v.title.SetText(room.Name)
	v.topic.SetText(room.Topic)
	v.setComposerEnabled(room.ID != "")
}

func (v *ChatView) SetMessages(messages []matrix.Message) {
	v.SetHistoryPage(matrix.HistoryPage{Messages: messages}, false)
}

func (v *ChatView) SetMessagesLoading(loading bool) {
	v.loadingMessages = loading
	v.updateEmptyState()
}

func (v *ChatView) SetSyncReady(ready bool) {
	if !ready && v.room.ID == "" {
		v.empty.SetText("Your chats are syncing from the homeserver. They will appear here as they load.")
	} else if v.room.ID == "" {
		v.empty.SetText("Select a chat on the left to read messages.")
	}
	v.empty.Refresh()
}

func (v *ChatView) SetLoadOlderHandler(handler func(roomID string, before int64)) {
	v.onLoadOlder = handler
	v.updateOlderButton()
}

func (v *ChatView) SetHistoryPage(page matrix.HistoryPage, prepend bool) {
	oldCount := len(v.history)
	oldAnchor := ""
	if oldCount > 0 {
		oldAnchor = v.history[0].ID
	}
	if prepend {
		combined := make([]matrix.Message, 0, len(page.Messages)+oldCount)
		combined = append(combined, v.history...)
		combined = append(combined, page.Messages...)
		v.history = deduplicateMessages(combined)
		sortMessages(v.history)
		if len(page.Messages) > 0 {
			v.loadedOlder = true
		}
	} else {
		v.history = append(v.history[:0], page.Messages...)
		v.loadedOlder = false
	}
	v.before = page.Before
	v.hasOlder = page.HasMore
	v.loadingOlder = false
	v.loadingMessages = false
	v.updateEmptyState()
	v.updateOlderButton()
	v.messages.Refresh()
	if prepend && oldCount > 0 && len(page.Messages) > 0 {
		for i, message := range v.history {
			if message.ID == oldAnchor {
				v.messages.ScrollTo(widget.ListItemID(i))
				break
			}
		}
	} else if len(v.history) > 0 {
		v.messages.ScrollToBottom()
	}
}

func (v *ChatView) RefreshLatest(page matrix.HistoryPage) {
	if !v.loadedOlder {
		v.SetHistoryPage(page, false)
		return
	}
	combined := make([]matrix.Message, 0, len(page.Messages)+len(v.history))
	combined = append(combined, page.Messages...)
	combined = append(combined, v.history...)
	v.history = deduplicateMessages(combined)
	sortMessages(v.history)
	v.hasOlder = page.HasMore || v.hasOlder
	v.loadingOlder = false
	v.loadingMessages = false
	v.updateEmptyState()
	v.updateOlderButton()
	v.messages.Refresh()
}

func (v *ChatView) OlderLoadFailed(message string) {
	v.loadingOlder = false
	v.SetStatus(message)
	v.updateOlderButton()
}

func (v *ChatView) requestOlder() {
	if v.loadingOlder || !v.hasOlder || v.before == 0 || v.room.ID == "" || v.onLoadOlder == nil {
		return
	}
	v.loadingOlder = true
	v.updateOlderButton()
	v.onLoadOlder(v.room.ID, v.before)
}

func (v *ChatView) updateOlderButton() {
	if v.loadingOlder {
		v.loadOlder.SetText("Loading...")
		v.loadOlder.Disable()
	} else if v.hasOlder && v.before != 0 && v.room.ID != "" && v.onLoadOlder != nil {
		v.loadOlder.SetText("Load older")
		v.loadOlder.Enable()
	} else {
		v.loadOlder.SetText("Load older")
		v.loadOlder.Disable()
	}
}

func (v *ChatView) updateEmptyState() {
	switch {
	case len(v.history) > 0:
		v.empty.Hide()
	case v.loadingMessages:
		v.empty.SetText("Loading messages for this chat...")
		v.empty.Show()
	case v.room.ID == "":
		v.empty.Show()
	default:
		v.empty.SetText("No messages are available in this chat yet. New messages will appear here when they sync.")
		v.empty.Show()
	}
	v.empty.Refresh()
}

func deduplicateMessages(messages []matrix.Message) []matrix.Message {
	seenIDs := make(map[string]struct{}, len(messages))
	seenTransactions := make(map[string]struct{}, len(messages))
	result := make([]matrix.Message, 0, len(messages))
	for _, message := range messages {
		if message.ID != "" {
			if _, exists := seenIDs[message.ID]; exists {
				continue
			}
		}
		if message.TransactionID != "" {
			if _, exists := seenTransactions[message.TransactionID]; exists {
				continue
			}
		}
		if message.ID != "" {
			seenIDs[message.ID] = struct{}{}
		}
		if message.TransactionID != "" {
			seenTransactions[message.TransactionID] = struct{}{}
		}
		result = append(result, message)
	}
	return result
}

func sortMessages(messages []matrix.Message) {
	sort.SliceStable(messages, func(i, j int) bool {
		if messages[i].Timestamp.Equal(messages[j].Timestamp) {
			if messages[i].TimelineCursor != messages[j].TimelineCursor {
				return messages[i].TimelineCursor < messages[j].TimelineCursor
			}
			return messages[i].ID < messages[j].ID
		}
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})
}

func (v *ChatView) SetStatus(message string) {
	v.status.SetText(message)
}

func (v *ChatView) submit() {
	text := strings.TrimSpace(v.input.Text)
	if v.room.ID == "" || text == "" || v.onSend == nil {
		return
	}
	v.input.SetText("")
	v.onSend(v.room.ID, text)
}

func (v *ChatView) setComposerEnabled(enabled bool) {
	if enabled {
		v.input.Enable()
		v.send.Enable()
	} else {
		v.input.Disable()
		v.send.Disable()
	}
}
