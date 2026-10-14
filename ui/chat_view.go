package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mychat/matrix"
)

type ChatView struct {
	title      *widget.Label
	topic      *widget.Label
	status     *widget.Label
	typing     *widget.Label
	empty      *widget.Label
	roomAvatar *avatarView
	loadOlder  *widget.Button
	replyLabel *widget.Label
	attach     *widget.Button
	completion *widget.Select
	favorite   *widget.Button
	mute       *widget.Button
	messages   *widget.List
	input      *composerEntry
	send       *widget.Button

	room            matrix.Room
	history         []matrix.Message
	avatars         *avatarCache
	messageAvatars  map[*fyne.Container]*avatarView
	retryButtons    map[*fyne.Container]*widget.Button
	onSend          func(roomID, text string)
	onSendMentions  func(roomID, text string, userIDs []string)
	onRetry         func(roomID, transactionID string)
	onTyping        func(roomID, text string)
	onSendReply     func(roomID, text, replyTo string)
	onReply         func(matrix.Message)
	onReact         func(matrix.Message, string)
	onEdit          func(matrix.Message)
	onRedact        func(matrix.Message)
	onLoadOlder     func(roomID string, before int64)
	onSearch        func(roomID string)
	onLeave         func(roomID string)
	onAttach        func(roomID string)
	onFavorite      func(roomID string)
	onMute          func(roomID string)
	typingTimer     *time.Timer
	before          int64
	hasOlder        bool
	loadingOlder    bool
	loadedOlder     bool
	loadingMessages bool
	syncReady       bool
	syncReadySet    bool
	statusText      string
	typingText      string
	replyTarget     *matrix.Message
	members         []matrix.Member
	completions     []completionOption
	mentionNames    map[string]string
}

type completionOption struct {
	label  string
	value  string
	userID string
}

func (v *ChatView) SetRetryHandler(handler func(roomID, transactionID string)) {
	v.onRetry = handler
	v.messages.Refresh()
}

func (v *ChatView) SetReplyHandler(send func(roomID, text, replyTo string), selectMessage func(matrix.Message)) {
	v.onSendReply = send
	v.onReply = selectMessage
}

func (v *ChatView) SetReactionHandler(handler func(matrix.Message, string)) {
	v.onReact = handler
}

func (v *ChatView) SetEditHandler(handler func(matrix.Message)) {
	v.onEdit = handler
}

func (v *ChatView) SetRedactHandler(handler func(matrix.Message)) {
	v.onRedact = handler
}

func (v *ChatView) SetTypingHandler(handler func(roomID, text string)) {
	v.onTyping = handler
}

func (v *ChatView) SetSearchHandler(handler func(roomID string)) {
	v.onSearch = handler
}

func (v *ChatView) SetLeaveHandler(handler func(roomID string)) {
	v.onLeave = handler
}

func (v *ChatView) SetAttachmentHandler(handler func(roomID string)) {
	v.onAttach = handler
}

func (v *ChatView) SetMentionSendHandler(handler func(roomID, text string, userIDs []string)) {
	v.onSendMentions = handler
}

func (v *ChatView) SetMembers(members []matrix.Member) {
	v.members = append(v.members[:0], members...)
	v.updateCompletions(v.input.Text)
}

func (v *ChatView) SetRoomPreferenceHandlers(favorite, mute func(roomID string)) {
	v.onFavorite = favorite
	v.onMute = mute
}

func NewChatView(onSend func(roomID, text string)) *ChatView {
	view := &ChatView{
		title:          widget.NewLabelWithStyle("Select a room", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		topic:          widget.NewLabel(""),
		status:         widget.NewLabel(""),
		typing:         widget.NewLabel(""),
		empty:          widget.NewLabel("Select a chat to see its messages."),
		roomAvatar:     newAvatarView(36),
		onSend:         onSend,
		messageAvatars: make(map[*fyne.Container]*avatarView),
		retryButtons:   make(map[*fyne.Container]*widget.Button),
		mentionNames:   make(map[string]string),
	}
	view.input = newComposerEntry(view.submit)
	view.input.SetPlaceHolder("Write a message... (Enter to send, Shift+Enter for a new line)")
	view.empty.Wrapping = fyne.TextWrapWord
	view.loadOlder = widget.NewButton("Load older", view.requestOlder)
	view.loadOlder.Disable()
	view.messages = widget.NewList(
		func() int { return len(view.history) },
		func() fyne.CanvasObject {
			avatar := newAvatarView(32)
			view.messageAvatars[avatar.root] = avatar
			details := widget.NewLabel("")
			body := widget.NewRichText()
			body.Wrapping = fyne.TextWrapWord
			reactions := widget.NewLabel("")
			retry := widget.NewButton("Retry", nil)
			retry.Hide()
			reply := widget.NewButton("Reply", nil)
			react := widget.NewButton("👍", nil)
			edit := widget.NewButton("Edit", nil)
			redact := widget.NewButton("Delete", nil)
			row := container.NewHBox(avatar.root, container.NewVBox(details, body, reactions), retry, reply, react, edit, redact)
			view.retryButtons[row] = retry
			return row
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(view.history) {
				return
			}
			message := view.history[id]
			box := object.(*fyne.Container)
			senderName := message.SenderName
			if senderName == "" {
				senderName = message.Sender
			}
			details := fmt.Sprintf("%s | %s", senderName, message.Timestamp.Local().Format(time.Kitchen))
			if message.Sending {
				details += " | Sending..."
			} else if message.SendError != "" {
				details += " | Failed to send: " + message.SendError
			}
			content := box.Objects[1].(*fyne.Container)
			content.Objects[0].(*widget.Label).SetText(details)
			content.Objects[1].(*widget.RichText).ParseMarkdown(message.Body)
			content.Objects[2].(*widget.Label).SetText(formatReactions(message.Reactions))
			retry := view.retryButtons[box]
			if message.SendError != "" && message.TransactionID != "" && view.onRetry != nil {
				retry.Show()
				retry.OnTapped = func() { view.onRetry(view.room.ID, message.TransactionID) }
			} else {
				retry.Hide()
				retry.OnTapped = nil
			}
			reply := box.Objects[3].(*widget.Button)
			reply.OnTapped = func() {
				if view.onReply != nil {
					view.onReply(message)
				}
			}
			react := box.Objects[4].(*widget.Button)
			react.OnTapped = func() {
				if view.onReact != nil {
					view.onReact(message, "👍")
				}
			}
			edit := box.Objects[5].(*widget.Button)
			if message.Own && message.ID != "" && !strings.HasPrefix(message.ID, "~") {
				edit.Show()
			} else {
				edit.Hide()
			}
			edit.OnTapped = func() {
				if view.onEdit != nil {
					view.onEdit(message)
				}
			}
			redact := box.Objects[6].(*widget.Button)
			if message.Own && message.ID != "" && !strings.HasPrefix(message.ID, "~") {
				redact.Show()
			} else {
				redact.Hide()
			}
			redact.OnTapped = func() {
				if view.onRedact != nil {
					view.onRedact(message)
				}
			}
			view.messageAvatars[box.Objects[0].(*fyne.Container)].set(message.Avatar, senderName, view.avatars)
		},
	)
	view.send = widget.NewButtonWithIcon("", theme.MailSendIcon(), view.submit)
	view.attach = widget.NewButton("Attach", func() {
		if view.onAttach != nil && view.room.ID != "" {
			view.onAttach(view.room.ID)
		}
	})
	view.favorite = widget.NewButton("Pin", func() {
		if view.onFavorite != nil && view.room.ID != "" {
			view.onFavorite(view.room.ID)
		}
	})
	view.mute = widget.NewButton("Mute", func() {
		if view.onMute != nil && view.room.ID != "" {
			view.onMute(view.room.ID)
		}
	})
	view.replyLabel = widget.NewLabel("")
	view.replyLabel.Hide()
	view.completion = widget.NewSelect(nil, view.applyCompletion)
	view.completion.Hide()
	view.input.OnChanged = func(text string) {
		view.updateCompletions(text)
		if view.typingTimer != nil {
			view.typingTimer.Stop()
		}
		if view.onTyping != nil && view.room.ID != "" {
			roomID := view.room.ID
			view.typingTimer = time.AfterFunc(350*time.Millisecond, func() {
				view.onTyping(roomID, text)
			})
		}
	}
	view.setComposerEnabled(false)
	return view
}

func (v *ChatView) Object() fyne.CanvasObject {
	titleBar := container.NewBorder(nil, nil, v.roomAvatar.root, container.NewHBox(
		widget.NewButton("Search", func() {
			if v.onSearch != nil && v.room.ID != "" {
				v.onSearch(v.room.ID)
			}
		}),
		widget.NewButton("Leave", func() {
			if v.onLeave != nil && v.room.ID != "" {
				v.onLeave(v.room.ID)
			}
		}),
		v.favorite,
		v.mute,
		v.loadOlder,
	), v.title)
	header := container.NewVBox(titleBar, v.topic, v.typing, v.status, widget.NewSeparator())
	composer := container.NewVBox(v.replyLabel, v.completion, container.NewBorder(nil, nil, v.attach, v.send, v.input))
	bottom := composer
	return container.NewBorder(header, bottom, nil, nil, container.NewStack(v.messages, container.NewCenter(v.empty)))
}

func (v *ChatView) SetRoom(room matrix.Room) {
	if v.typingTimer != nil {
		v.typingTimer.Stop()
	}
	v.replyLabel.Hide()
	v.replyTarget = nil
	clear(v.mentionNames)
	v.members = nil
	v.completion.Hide()
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
	if v.room == room {
		return
	}
	v.room = room
	v.title.SetText(room.Name)
	if room.Favorite {
		v.favorite.SetText("Unpin")
	} else {
		v.favorite.SetText("Pin")
	}
	if room.Muted {
		v.mute.SetText("Unmute")
	} else {
		v.mute.SetText("Mute")
	}
	v.topic.SetText(room.Topic)
	v.roomAvatar.set(room.Avatar, room.Name, v.avatars)
	v.setComposerEnabled(room.ID != "")
}

func (v *ChatView) SetAvatarCache(cache *avatarCache) {
	v.avatars = cache
	v.roomAvatar.set(v.room.Avatar, v.room.Name, cache)
	v.messages.Refresh()
}

func (v *ChatView) SetMessages(messages []matrix.Message) {
	v.SetHistoryPage(matrix.HistoryPage{Messages: messages}, false)
}

func (v *ChatView) SetMessagesLoading(loading bool) {
	v.loadingMessages = loading
	v.updateEmptyState()
}

func (v *ChatView) SetSyncReady(ready bool) {
	if v.syncReadySet && v.syncReady == ready {
		return
	}
	v.syncReady = ready
	v.syncReadySet = true
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
	if v.statusText == message {
		return
	}
	v.statusText = message
	v.status.SetText(message)
}

func (v *ChatView) SetTypingStatus(users []string) {
	message := ""
	switch len(users) {
	case 1:
		message = users[0] + " is typing…"
	default:
		if len(users) > 1 {
			message = fmt.Sprintf("%d people are typing…", len(users))
		}
	}
	if v.typingText != message {
		v.typingText = message
		v.typing.SetText(message)
	}
}

func (v *ChatView) submit() {
	text := strings.TrimSpace(v.input.Text)
	if v.room.ID == "" || text == "" || (v.onSend == nil && v.onSendMentions == nil && v.onSendReply == nil) {
		return
	}
	mentionIDs := make([]string, 0, len(v.mentionNames))
	for displayName, userID := range v.mentionNames {
		if strings.Contains(text, "@"+displayName) {
			mentionIDs = append(mentionIDs, userID)
		}
	}
	v.input.SetText("")
	v.completion.Hide()
	if v.replyTarget != nil && v.onSendReply != nil {
		v.onSendReply(v.room.ID, text, v.replyTarget.ID)
		v.replyTarget = nil
		v.replyLabel.Hide()
	} else if v.onSendMentions != nil {
		v.onSendMentions(v.room.ID, text, mentionIDs)
	} else if v.onSend != nil {
		v.onSend(v.room.ID, text)
	}
	clear(v.mentionNames)
}

func (v *ChatView) selectReply(message matrix.Message) {
	if message.ID == "" || strings.HasPrefix(message.ID, "~") {
		return
	}
	v.replyTarget = &message
	v.replyLabel.SetText("Replying to " + message.SenderName + ": " + message.Body)
	v.replyLabel.Show()
}

func formatReactions(reactions map[string]int) string {
	if len(reactions) == 0 {
		return ""
	}
	keys := make([]string, 0, len(reactions))
	for key := range reactions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s %d", key, reactions[key]))
	}
	return strings.Join(values, "  ")
}

func (v *ChatView) updateCompletions(text string) {
	start := strings.LastIndexAny(text, " \t\n") + 1
	token := text[start:]
	if len(token) < 2 || (token[0] != ':' && token[0] != '@') {
		v.completions = nil
		v.completion.Hide()
		return
	}
	options := make([]completionOption, 0, 8)
	if token[0] == ':' {
		for name, emoji := range map[string]string{
			"smile": "😄", "smiley": "😃", "heart": "❤️", "thumbsup": "👍",
			"laughing": "😆", "cry": "😢", "wink": "😉", "fire": "🔥",
		} {
			if strings.HasPrefix(name, strings.TrimPrefix(token[1:], ":")) {
				options = append(options, completionOption{label: ":" + name + ": " + emoji, value: emoji})
			}
		}
	} else {
		query := strings.ToLower(strings.TrimPrefix(token[1:], "@"))
		for _, member := range v.members {
			if strings.Contains(strings.ToLower(member.DisplayName), query) || strings.Contains(strings.ToLower(member.ID), query) {
				options = append(options, completionOption{
					label:  member.DisplayName + " (" + member.ID + ")",
					value:  "@" + member.DisplayName,
					userID: member.ID,
				})
			}
			if len(options) >= 12 {
				break
			}
		}
	}
	sort.Slice(options, func(i, j int) bool { return options[i].label < options[j].label })
	if len(options) == 0 {
		v.completions = nil
		v.completion.Hide()
		return
	}
	v.completions = options
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = option.label
	}
	v.completion.Options = labels
	v.completion.Selected = ""
	v.completion.Refresh()
	v.completion.Show()
}

func (v *ChatView) applyCompletion(label string) {
	for _, option := range v.completions {
		if option.label != label {
			continue
		}
		text := v.input.Text
		start := strings.LastIndexAny(text, " \t\n") + 1
		value := option.value + " "
		v.input.SetText(text[:start] + value)
		v.input.CursorRow = strings.Count(v.input.Text, "\n")
		lastLine := v.input.Text[strings.LastIndex(v.input.Text, "\n")+1:]
		v.input.CursorColumn = len([]rune(lastLine))
		if option.userID != "" {
			v.mentionNames[option.value[1:]] = option.userID
		}
		v.completion.Hide()
		return
	}
}

func (v *ChatView) setComposerEnabled(enabled bool) {
	if enabled {
		v.input.Enable()
		v.send.Enable()
		v.attach.Enable()
		v.favorite.Enable()
		v.mute.Enable()
	} else {
		v.input.Disable()
		v.send.Disable()
		v.attach.Disable()
		v.favorite.Disable()
		v.mute.Disable()
	}
}

type composerEntry struct {
	*widget.Entry
	shiftDown bool
	onSend    func()
}

func newComposerEntry(onSend func()) *composerEntry {
	entry := &composerEntry{
		Entry:  widget.NewMultiLineEntry(),
		onSend: onSend,
	}
	entry.SetMinRowsVisible(2)
	entry.Wrapping = fyne.TextWrapWord
	return entry
}

func (e *composerEntry) KeyDown(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		e.shiftDown = true
	}
	e.Entry.KeyDown(key)
}

func (e *composerEntry) KeyUp(key *fyne.KeyEvent) {
	e.Entry.KeyUp(key)
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		e.shiftDown = false
	}
}

func (e *composerEntry) TypedKey(key *fyne.KeyEvent) {
	if !e.shiftDown && (key.Name == fyne.KeyReturn || key.Name == fyne.KeyEnter) {
		if e.onSend != nil {
			e.onSend()
		}
		return
	}
	e.Entry.TypedKey(key)
}
