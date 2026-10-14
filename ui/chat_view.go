package ui

import (
	"fmt"
	"image/color"
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
	title       *widget.Label
	topic       *widget.Label
	status      *widget.Label
	typing      *widget.Label
	empty       *widget.Label
	roomAvatar  *avatarView
	loadOlder   *widget.Button
	replyLabel  *widget.Label
	replyCancel *widget.Button
	replyCard   *widget.Button
	editLabel   *widget.Label
	editCancel  *widget.Button
	attach      *widget.Button
	emoji       *widget.Button
	completion  *widget.Select
	favorite    *widget.Button
	mute        *widget.Button
	messages    *widget.List
	input       *composerEntry
	send        *widget.Button

	room            matrix.Room
	history         []matrix.Message
	historyByID     map[string]int
	avatars         *avatarCache
	messageAvatars  map[*fyne.Container]*avatarView
	bubbles         map[*fyne.Container]*messageBubble
	retryButtons    map[*fyne.Container]*widget.Button
	downloadButtons map[*fyne.Container]*widget.Button
	media           *mediaCache
	mediaThumbs     map[*fyne.Container]*mediaThumbView
	onSend          func(roomID, text string)
	onSendMentions  func(roomID, text string, userIDs []string)
	onRetry         func(roomID, transactionID string)
	onTyping        func(roomID, text string)
	onSendReply     func(roomID, text, replyTo string)
	onReply         func(matrix.Message)
	onReact         func(matrix.Message, string)
	onReactionPick  func(matrix.Message)
	onEdit          func(matrix.Message)
	onEditSend      func(roomID, text, editID string)
	onRedact        func(matrix.Message)
	onLoadOlder     func(roomID string, before int64)
	onSearch        func(roomID string)
	onLeave         func(roomID string)
	onAttach        func(roomID string)
	onEmoji         func()
	onDownload      func(matrix.Message)
	onFavorite      func(roomID string)
	onMute          func(roomID string)
	onCopy          func(text string)
	onOpenMedia     func(matrix.Message)
	onScrollTop     func()
	typingTimer     *time.Timer
	before          int64
	lastTopLoad     time.Time
	hasOlder        bool
	loadingOlder    bool
	loadedOlder     bool
	loadingMessages bool
	syncReady       bool
	syncReadySet    bool
	statusText      string
	typingText      string
	replyTarget     *matrix.Message
	editTarget      *matrix.Message
	members         []matrix.Member
	completions     []completionOption
	mentionNames    map[string]string
	showAvatars     bool
	compact         bool
	time24Hour      bool
	relativeTime    bool
	bubbleColor     color.Color
}

// bubblePadding is the inset applied inside a message bubble so its border and
// background don't touch the row edges.
const bubblePadding = 8

// bubbleMaxWidth caps the width of an outgoing (own) message bubble so long
// messages wrap instead of stretching across the full timeline.
const bubbleMaxWidth = 360

func (v *ChatView) SetShowAvatars(show bool) {
	v.showAvatars = show
	v.messages.Refresh()
}

// SetBubbleColor sets the fill color used for outgoing (own) message bubbles.
func (v *ChatView) SetBubbleColor(color color.Color) {
	v.bubbleColor = color
	v.messages.Refresh()
}

func (v *ChatView) Set24HourTime(enabled bool) {
	v.time24Hour = enabled
	v.messages.Refresh()
}

func (v *ChatView) SetRelativeTimestamps(enabled bool) {
	v.relativeTime = enabled
	v.messages.Refresh()
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

// SetReactionPickerHandler opens an emoji picker for a message (hover "+" action).
func (v *ChatView) SetReactionPickerHandler(handler func(matrix.Message)) {
	v.onReactionPick = handler
}

// SetCopyHandler copies text (code blocks) to the clipboard.
func (v *ChatView) SetCopyHandler(handler func(string)) {
	v.onCopy = handler
}

// SetMediaOpenHandler opens the fullscreen media viewer.
func (v *ChatView) SetMediaOpenHandler(handler func(matrix.Message)) {
	v.onOpenMedia = handler
}

// SetScrollTopHandler loads older history when the timeline reaches the top.
func (v *ChatView) SetScrollTopHandler(handler func()) {
	v.onScrollTop = handler
}

// SetCompact toggles compact (IRC-style) vs bubble timeline layout.
func (v *ChatView) SetCompact(compact bool) {
	v.compact = compact
	v.messages.Refresh()
}

// OlderCursor exposes the pagination cursor for scroll-top auto-load.
func (v *ChatView) OlderCursor() int64 { return v.before }

// CanLoadOlder reports whether older history can be requested.
func (v *ChatView) CanLoadOlder() bool { return v.hasOlder && !v.loadingOlder }

func (v *ChatView) SetEditHandler(handler func(matrix.Message)) {
	v.onEdit = handler
}

func (v *ChatView) SetEditSendHandler(handler func(roomID, text, editID string)) {
	v.onEditSend = handler
}

func (v *ChatView) SetRedactHandler(handler func(matrix.Message)) {
	v.onRedact = handler
}

func (v *ChatView) SetTypingHandler(handler func(roomID, text string)) {
	v.onTyping = handler
}

func (v *ChatView) SetSendOnCtrlEnter(enabled bool) {
	v.input.sendOnCtrlEnter = enabled
	if enabled {
		v.input.SetPlaceHolder("Write a message... (Ctrl+Enter to send, Enter for a new line)")
	} else {
		v.input.SetPlaceHolder("Write a message... (Enter to send, Shift+Enter for a new line)")
	}
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

func (v *ChatView) SetEmojiHandler(handler func()) {
	v.onEmoji = handler
}

func (v *ChatView) SetDownloadHandler(handler func(matrix.Message)) {
	v.onDownload = handler
	v.messages.Refresh()
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
		title:           widget.NewLabelWithStyle("Select a room", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		topic:           widget.NewLabel(""),
		status:          widget.NewLabel(""),
		typing:          widget.NewLabel(""),
		empty:           widget.NewLabel("Select a chat to see its messages."),
		roomAvatar:      newAvatarView(36),
		onSend:          onSend,
		messageAvatars:  make(map[*fyne.Container]*avatarView),
		bubbles:         make(map[*fyne.Container]*messageBubble),
		retryButtons:    make(map[*fyne.Container]*widget.Button),
		downloadButtons: make(map[*fyne.Container]*widget.Button),
		mediaThumbs:     make(map[*fyne.Container]*mediaThumbView),
		bubbleColor:     theme.Color(theme.ColorNameSecondary),
		mentionNames:    make(map[string]string),
		showAvatars:     true,
	}
	view.input = newComposerEntry(view.submit)
	view.input.SetPlaceHolder("Write a message... (Enter to send, Shift+Enter for a new line)")
	view.input.SetMinRowsVisible(1)
	view.empty.Wrapping = fyne.TextWrapWord
	view.loadOlder = widget.NewButton("Load older", view.requestOlder)
	view.loadOlder.Disable()
	view.messages = widget.NewList(
		func() int { return len(view.history) },
		func() fyne.CanvasObject {
			avatar := newAvatarView(32)
			view.messageAvatars[avatar.root] = avatar
			details := widget.NewLabel("")
			replyCard := widget.NewButton("", nil)
			replyCard.Hide()
			body := widget.NewRichText()
			body.Wrapping = fyne.TextWrapWord
			reactions := widget.NewLabel("")
			retry := widget.NewButton("Retry", nil)
			retry.Hide()
			reply := widget.NewButton("Reply", nil)
			react := widget.NewButton("👍", nil)
			reactMore := widget.NewButton("+", nil)
			edit := widget.NewButton("Edit", nil)
			redact := widget.NewButton("Delete", nil)
			download := widget.NewButton("Download", nil)
			download.Hide()
			thumb := newMediaThumbView()
			bubble := newMessageBubble()
			row := container.NewHBox(avatar.root, bubble.root, container.NewVBox(details, replyCard, body, thumb.root, reactions), retry, reply, react, reactMore, edit, redact, download)
			view.retryButtons[row] = retry
			view.downloadButtons[row] = download
			view.mediaThumbs[row] = thumb
			return row
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(view.history) {
				return
			}
			if id == 0 {
				view.loadOlderIfAtTop()
			}
			message := view.history[id]
			box := object.(*fyne.Container)
			senderName := message.SenderName
			if senderName == "" {
				senderName = message.Sender
			}
			timeFormat := time.Kitchen
			if view.time24Hour {
				timeFormat = "15:04"
			}
			avatar := box.Objects[0].(*fyne.Container)
			showSender := true
			if id > 0 {
				previous := view.history[id-1]
				showSender = !shouldGroupMessages(previous, message)
			}
			detailsText := messageTimestamp(message.Timestamp.Local(), timeFormat, view.relativeTime, time.Now())
			if showSender {
				detailsText = senderName + " | " + detailsText
			}
			if message.Edited {
				detailsText += " | edited"
				if len(message.EditHistory) > 0 {
					detailsText += fmt.Sprintf(" (%d prior)", len(message.EditHistory))
				}
			}
			if message.Sending {
				detailsText += " | ⏳ pending"
			}
			if message.DecryptionError != "" {
				detailsText += " | 🛡 decryption failed"
			} else if message.Encrypted {
				detailsText += " | 🔒 encrypted"
			}
			details := detailsText
			if message.Sending {
				details += " | Sending..."
			} else if message.SendError != "" {
				details += " | ⚠ Failed to send: " + message.SendError
			}
			content := box.Objects[1].(*fyne.Container)
			detailsLabel := content.Objects[0].(*widget.Label)
			// Compact (IRC-style) rows drop the meta line on grouped continuation
			// messages unless it carries status information (edited/pending/failed/encryption).
			hideMeta := view.compact && !showSender && !message.Edited && !message.Sending &&
				message.SendError == "" && message.DecryptionError == "" && !message.Encrypted
			if hideMeta {
				detailsLabel.Hide()
			} else {
				detailsLabel.SetText(details)
				detailsLabel.Show()
			}
			if quote := view.replyQuote(message); quote != "" {
				replyCard := content.Objects[1].(*widget.Button)
				replyCard.SetText(quote)
				replyCard.Show()
				replyCard.OnTapped = func() { view.scrollToMessage(message.ReplyToEventID) }
			} else {
				replyCard := content.Objects[1].(*widget.Button)
				replyCard.Hide()
				replyCard.OnTapped = nil
			}
			body := message.Body
			if message.Redacted {
				body = "This message was deleted"
			} else if message.DecryptionError != "" {
				body = "Unable to decrypt this message"
			} else if code := extractCodeBlock(message.Body); code != "" && view.onCopy != nil {
				body = message.Body + "\n\n[Copy code available via message menu]"
			}
			if len([]rune(body)) > maxMessageBodyLen {
				body = string([]rune(body)[:maxMessageBodyLen-1]) + "…"
			}
			content.Objects[2].(*widget.RichText).ParseMarkdown(body)
			if thumb, ok := view.mediaThumbs[box]; ok {
				if message.MediaURI != "" && strings.HasPrefix(message.MediaMIME, "image/") &&
					!message.Redacted && message.DecryptionError == "" {
					tapMessage := message
					thumb.set(message.MediaURI, view.room.ID, message.MediaWidth, message.MediaHeight, view.media, func() {
						if view.onOpenMedia != nil {
							view.onOpenMedia(tapMessage)
						}
					})
				} else {
					thumb.hide()
				}
			}
			content.Objects[4].(*widget.Label).SetText(formatReactions(message.Reactions, message.Reacted))
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
			reactMore := box.Objects[5].(*widget.Button)
			reactMore.OnTapped = func() {
				if view.onReactionPick != nil {
					view.onReactionPick(message)
				} else if view.onReact != nil {
					view.onReact(message, "❤️")
				}
			}
			edit := box.Objects[6].(*widget.Button)
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
			redact := box.Objects[7].(*widget.Button)
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
			download := view.downloadButtons[box]
			if message.MediaURI != "" && view.onDownload != nil {
				switch {
				case strings.HasPrefix(message.MediaMIME, "image/"):
					download.SetText("Preview")
				case strings.HasPrefix(message.MediaMIME, "audio/") || strings.HasPrefix(message.MediaMIME, "video/"):
					label := fileKindLabel(message.MediaMIME)
					if message.MediaDurationMs > 0 {
						label += " · " + formatDuration(message.MediaDurationMs)
					} else if message.MediaSize > 0 {
						label += " · " + humanBytes(int64(message.MediaSize))
					}
					download.SetText("Play " + label)
				case message.MediaSize > 0:
					download.SetText(fmt.Sprintf("%s · %s", fileKindLabel(message.MediaMIME), humanBytes(int64(message.MediaSize))))
				default:
					download.SetText("Download")
				}
				download.Show()
				download.OnTapped = func() { view.onDownload(message) }
			} else {
				download.Hide()
				download.OnTapped = nil
			}
			if view.showAvatars && showSender && !view.compact {
				avatar.Show()
				view.messageAvatars[avatar].set(message.Avatar, senderName, view.avatars)
			} else {
				avatar.Hide()
			}
			// Outgoing (own) messages render as a right-aligned colored bubble;
			// incoming messages leave the bubble transparent so it costs nothing
			// in recycled rows. The bubble's trailing corner is rounded and it is
			// capped at bubbleMaxWidth so long text wraps instead of stretching.
			bubble := view.bubbles[box]
			if message.Own && !view.compact {
				bubble.show(view.bubbleColor)
			} else {
				bubble.hide()
			}
			if box.Size().Width > 0 {
				bubble.body.Resize(fyne.NewSize(
					box.Size().Width-bubblePadding*2,
					box.Size().Height-bubblePadding*2,
				))
				bubble.body.Move(fyne.NewPos(bubblePadding, bubblePadding))
			}
		},
	)
	// Older history is requested from the row-0 update callback (loadOlderIfAtTop),
	// which fires whenever the first message is visible: on refresh and when the
	// user scrolls back to the top. widget.List in Fyne v2.8 exposes no public
	// scroll callback, so row visibility is the reliable signal.
	view.send = widget.NewButtonWithIcon("", theme.MailSendIcon(), view.submit)
	view.attach = widget.NewButton("Attach", func() {
		if view.onAttach != nil && view.room.ID != "" {
			view.onAttach(view.room.ID)
		}
	})
	view.emoji = widget.NewButton("Emoji", func() {
		if view.onEmoji != nil {
			view.onEmoji()
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
	view.replyCard = widget.NewButton("", func() {
		if view.replyTarget != nil && view.replyTarget.ID != "" {
			view.scrollToMessage(view.replyTarget.ID)
		}
	})
	view.replyCard.Hide()
	view.replyCancel = widget.NewButton("×", view.clearReply)
	view.replyCancel.Hide()
	view.editLabel = widget.NewLabel("")
	view.editLabel.Hide()
	view.editCancel = widget.NewButton("Cancel edit", view.cancelEdit)
	view.editCancel.Hide()
	view.completion = widget.NewSelect(nil, view.applyCompletion)
	view.completion.Hide()
	view.input.OnChanged = func(text string) {
		view.updateCompletions(text)
		view.updateComposerState()
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
	replyBanner := container.NewBorder(nil, nil, nil, v.replyCancel, container.NewVBox(v.replyLabel, v.replyCard))
	editBanner := container.NewBorder(nil, nil, nil, v.editCancel, v.editLabel)
	composer := container.NewVBox(replyBanner, editBanner, v.completion, container.NewBorder(nil, nil, container.NewHBox(v.attach, v.emoji), v.send, v.input))
	bottom := composer
	return container.NewBorder(header, bottom, nil, nil, container.NewStack(v.messages, container.NewCenter(v.empty)))
}

func (v *ChatView) SetRoom(room matrix.Room) {
	if v.typingTimer != nil {
		v.typingTimer.Stop()
	}
	v.replyLabel.Hide()
	v.replyCancel.Hide()
	if v.replyCard != nil {
		v.replyCard.Hide()
	}
	v.replyTarget = nil
	v.clearEdit()
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
	v.updateComposerState()
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

// SetMediaCache enables inline image thumbnails in the timeline.
func (v *ChatView) SetMediaCache(cache *mediaCache) {
	v.media = cache
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
	v.reindexHistory()
	v.resolveReplyPreviews()
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

// loadOlderIfAtTop fetches older history when the first timeline row is visible.
// It is called from the row-0 update callback, i.e. whenever the top of the
// conversation is on screen, and is throttled to at most one request per second.
func (v *ChatView) loadOlderIfAtTop() {
	if !v.CanLoadOlder() || v.before == 0 || v.room.ID == "" {
		return
	}
	if time.Since(v.lastTopLoad) < time.Second {
		return
	}
	v.lastTopLoad = time.Now()
	if v.onScrollTop != nil {
		v.loadingOlder = true
		v.updateOlderButton()
		v.onScrollTop()
		return
	}
	v.requestOlder()
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

func (v *ChatView) reindexHistory() {
	if v.historyByID == nil {
		v.historyByID = make(map[string]int)
	} else {
		clear(v.historyByID)
	}
	for i, message := range v.history {
		if message.ID != "" {
			v.historyByID[message.ID] = i
		}
	}
}

// resolveReplyPreviews fills reply sender/preview from loaded history.
func (v *ChatView) resolveReplyPreviews() {
	if len(v.history) == 0 {
		return
	}
	byID := make(map[string]matrix.Message, len(v.history))
	for _, message := range v.history {
		if message.ID != "" {
			byID[message.ID] = message
		}
	}
	for i, message := range v.history {
		if message.ReplyToEventID == "" || message.ReplyToPreview != "" {
			continue
		}
		if original, ok := byID[message.ReplyToEventID]; ok {
			sender := original.SenderName
			if sender == "" {
				sender = original.Sender
			}
			v.history[i].ReplyToSender = sender
			v.history[i].ReplyToPreview = truncatePreview(original.Body, 80)
		}
	}
}

func truncatePreview(body string, limit int) string {
	body = strings.ReplaceAll(strings.TrimSpace(body), "\n", " ")
	if limit <= 0 {
		return body
	}
	runes := []rune(body)
	if len(runes) <= limit {
		return body
	}
	if limit <= 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

// replyQuote renders the reply card text for a message.
func (v *ChatView) replyQuote(message matrix.Message) string {
	if message.ReplyToEventID == "" {
		return ""
	}
	sender := message.ReplyToSender
	if sender == "" {
		if index, ok := v.historyByID[message.ReplyToEventID]; ok && index >= 0 && index < len(v.history) {
			original := v.history[index]
			sender = original.SenderName
			if sender == "" {
				sender = original.Sender
			}
			preview := truncatePreview(original.Body, 80)
			if preview == "" {
				return "↩ Reply"
			}
			return "↩ " + sender + ": " + preview
		}
		return "↩ Reply to " + message.ReplyToEventID
	}
	preview := message.ReplyToPreview
	if preview == "" {
		return "↩ " + sender
	}
	return "↩ " + sender + ": " + preview
}

// scrollToMessage jumps the timeline to the replied-to event.
func (v *ChatView) scrollToMessage(eventID string) {
	if eventID == "" {
		return
	}
	index, ok := v.historyByID[eventID]
	if !ok {
		// Best effort: request older history which may contain the target.
		v.requestOlder()
		return
	}
	v.messages.ScrollTo(widget.ListItemID(index))
}

// ScrollToMessage exposes reply navigation for tests and the app layer.
func (v *ChatView) ScrollToMessage(eventID string) { v.scrollToMessage(eventID) }

// ReplyPreviewFor exposes the reply card text for tests.
func (v *ChatView) ReplyPreviewFor(message matrix.Message) string { return v.replyQuote(message) }

// extractCodeBlock returns the first fenced code block body, if any.
func extractCodeBlock(body string) string {
	start := strings.Index(body, "```")
	if start < 0 {
		return ""
	}
	rest := body[start+3:]
	// Skip optional language tag on the opening fence line.
	if newline := strings.Index(rest, "\n"); newline >= 0 {
		rest = rest[newline+1:]
	} else {
		return ""
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// formatReactions renders pills and marks own reactions with ●.
func formatReactions(counts map[string]int, reacted map[string]bool) string {
	if len(counts) == 0 {
		return ""
	}
	emojis := make([]string, 0, len(counts))
	for emoji := range counts {
		emojis = append(emojis, emoji)
	}
	sort.Strings(emojis)
	parts := make([]string, 0, len(emojis))
	for _, emoji := range emojis {
		part := fmt.Sprintf("%s %d", emoji, counts[emoji])
		if reacted[emoji] {
			part = "● " + part
		}
		parts = append(parts, "["+part+"]")
	}
	return strings.Join(parts, " ")
}

func shouldGroupMessages(previous, current matrix.Message) bool {
	delta := current.Timestamp.Sub(previous.Timestamp)
	return previous.Sender == current.Sender && delta >= 0 && delta <= 5*time.Minute
}

func messageTimestamp(timestamp time.Time, clockFormat string, relative bool, now time.Time) string {
	if !relative {
		return timestamp.Format(clockFormat)
	}
	delta := now.Sub(timestamp)
	switch {
	case delta < 0:
		return timestamp.Format(clockFormat)
	case delta < time.Minute:
		return "now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	default:
		return timestamp.Format("Jan 2")
	}
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
	if v.room.ID == "" || text == "" {
		return
	}
	if v.editTarget != nil {
		if v.onEditSend == nil {
			return
		}
		editID := v.editTarget.ID
		roomID := v.room.ID
		v.clearEdit()
		v.input.SetText("")
		v.onEditSend(roomID, text, editID)
		return
	}
	if v.onSend == nil && v.onSendMentions == nil && v.onSendReply == nil {
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
	sender := message.SenderName
	if sender == "" {
		sender = message.Sender
	}
	preview := truncatePreview(message.Body, 100)
	v.replyLabel.SetText("Replying to " + sender)
	v.replyLabel.Show()
	v.replyCard.SetText("↩ " + preview)
	v.replyCard.Show()
	v.replyCancel.Show()
	v.editTarget = nil
	v.editLabel.Hide()
	v.editCancel.Hide()
}

func (v *ChatView) clearReply() {
	v.replyTarget = nil
	v.replyLabel.Hide()
	if v.replyCard != nil {
		v.replyCard.Hide()
	}
	v.replyCancel.Hide()
}

func (v *ChatView) BeginEdit(message matrix.Message) {
	if message.ID == "" || strings.HasPrefix(message.ID, "~") {
		return
	}
	v.clearReply()
	v.editTarget = &message
	v.editLabel.SetText("Editing message")
	v.editLabel.Show()
	v.editCancel.Show()
	v.input.SetText(message.Body)
	v.updateComposerState()
}

func (v *ChatView) clearEdit() {
	v.editTarget = nil
	if v.editLabel != nil {
		v.editLabel.Hide()
	}
	if v.editCancel != nil {
		v.editCancel.Hide()
	}
}

func (v *ChatView) cancelEdit() {
	v.clearEdit()
	v.input.SetText("")
	v.updateComposerState()
}

func fileKindLabel(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case mimeType == "":
		return "File"
	case strings.HasPrefix(mimeType, "image/"):
		return "Image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "Audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "Video"
	case strings.Contains(mimeType, "pdf"):
		return "PDF"
	case strings.Contains(mimeType, "zip") || strings.Contains(mimeType, "archive"):
		return "Archive"
	default:
		return "File"
	}
}

func formatDuration(durationMs int) string {
	if durationMs <= 0 {
		return "0:00"
	}
	seconds := durationMs / 1000
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
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
		v.attach.Enable()
		v.emoji.Enable()
		v.favorite.Enable()
		v.mute.Enable()
		v.updateComposerState()
	} else {
		v.input.Disable()
		v.send.Disable()
		v.attach.Disable()
		v.emoji.Disable()
		v.favorite.Disable()
		v.mute.Disable()
	}
}

func (v *ChatView) updateComposerState() {
	text := strings.TrimSpace(v.input.Text)
	canSend := v.room.ID != "" && text != ""
	if canSend {
		v.send.Enable()
		v.send.Importance = widget.HighImportance
	} else {
		v.send.Disable()
		v.send.Importance = widget.MediumImportance
	}
	lineCount := strings.Count(v.input.Text, "\n") + 1
	if lineCount < 1 {
		lineCount = 1
	}
	if lineCount > 5 {
		lineCount = 5
	}
	v.input.SetMinRowsVisible(lineCount)
}

type composerEntry struct {
	*widget.Entry
	shiftDown       bool
	sendOnCtrlEnter bool
	onSend          func()
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
	if !e.shiftDown && !e.sendOnCtrlEnter && (key.Name == fyne.KeyReturn || key.Name == fyne.KeyEnter) {
		if e.onSend != nil {
			e.onSend()
		}
		return
	}
	e.Entry.TypedKey(key)
}

const maxMessageBodyLen = 2000

// messageBubble renders the colored speech-bubble background for a timeline
// row. It sits between the sender avatar and the metadata/body column so that
// outgoing (own) messages visually shift right while incoming messages keep
// their metadata on the left. The bubble is transparent when empty to avoid
// wasting space in recycled list rows.
type messageBubble struct {
	root *fyne.Container
	body *canvas.Rectangle
}

func newMessageBubble() *messageBubble {
	body := canvas.NewRectangle(nil)
	bubble := &messageBubble{root: container.NewStack(body), body: body}
	bubble.root.Hide()
	return bubble
}

// show renders the bubble for an outgoing (own) message with a tinted fill and
// a rounded trailing corner. hide clears it so recycled rows stay lightweight.
func (b *messageBubble) show(color color.Color) {
	b.body.FillColor = color
	b.body.Show()
	b.root.Show()
}

func (b *messageBubble) hide() {
	b.body.Hide()
	b.root.Hide()
}
