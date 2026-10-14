package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"mychat/matrix"
)

type DesktopApp struct {
	backend *matrix.Client
	fyneApp fyne.App
	window  fyne.Window
	trayApp desktop.App

	ctx              context.Context
	cancel           context.CancelFunc
	roomList         *RoomList
	chat             *ChatView
	avatars          *avatarCache
	selectedRoom     string
	loadSequence     uint64
	syncReady        bool
	themeDark        bool
	lastAnsweredRoom string
	quickSwitcher    dialog.Dialog
	shutdownOnce     sync.Once
}

func (d *DesktopApp) sendMessageWithMentions(roomID, text string, userIDs []string) {
	if len(userIDs) == 0 {
		d.sendMessage(roomID, text)
		return
	}
	go func() {
		if err := d.backend.SendMessageWithMentions(d.ctx, roomID, text, userIDs); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() { d.reloadMessages(roomID) })
	}()
}

func NewApp(backend *matrix.Client) *DesktopApp {
	fyneApp := app.NewWithID("fi.mau.fyne-matrix")
	fyneApp.SetIcon(iconResource())
	window := fyneApp.NewWindow("Matrix")
	ctx, cancel := context.WithCancel(context.Background())
	desktopApp := &DesktopApp{
		backend: backend,
		fyneApp: fyneApp,
		window:  window,
		ctx:     ctx,
		cancel:  cancel,
	}
	desktopApp.avatars = newAvatarCache(ctx, backend.DownloadAvatar)
	desktopApp.applyTheme(fyneApp.Preferences().StringWithFallback("appearance.theme", "System default"))
	window.Resize(fyne.NewSize(1100, 720))
	window.SetCloseIntercept(func() {
		desktopApp.closeWindow()
	})
	window.SetOnDropped(func(_ fyne.Position, items []fyne.URI) {
		if desktopApp.selectedRoom == "" {
			dialog.ShowError(errors.New("select a chat before dropping attachments"), window)
			return
		}
		for _, item := range items {
			reader, err := storage.Reader(item)
			if err != nil {
				dialog.ShowError(fmt.Errorf("open dropped file %q: %w", item.Name(), err), window)
				continue
			}
			desktopApp.uploadAttachment(desktopApp.selectedRoom, item.Name(), reader)
		}
	})
	if trayApp, ok := fyneApp.(desktop.App); ok {
		desktopApp.trayApp = trayApp
		trayApp.SetSystemTrayIcon(iconResource())
		trayApp.SetSystemTrayWindow(window)
		trayApp.SetSystemTrayMenu(fyne.NewMenu("Gomuks Desktop",
			fyne.NewMenuItem("Open", func() {
				window.Show()
				window.RequestFocus()
			}),
			fyne.NewMenuItem("Mark All as Read", desktopApp.markAllRead),
			fyne.NewMenuItem("Quit", desktopApp.quit),
		))
	}
	return desktopApp
}

func (d *DesktopApp) closeWindow() {
	if d.trayApp != nil && d.fyneApp.Preferences().Bool("window.minimize_to_tray") {
		d.window.Hide()
		return
	}
	d.quit()
}

func (d *DesktopApp) quit() {
	d.shutdownOnce.Do(func() {
		d.cancel()
		go func() {
			d.backend.Close()
			fyne.Do(d.fyneApp.Quit)
		}()
	})
}

func (d *DesktopApp) markAllRead() {
	go func() {
		var firstErr error
		for _, room := range d.backend.Rooms() {
			page, err := d.backend.HistoryPage(d.ctx, room.ID, 0)
			if err != nil {
				firstErr = errors.Join(firstErr, err)
				continue
			}
			for i := len(page.Messages) - 1; i >= 0; i-- {
				message := page.Messages[i]
				if message.ID == "" || strings.HasPrefix(message.ID, "~") {
					continue
				}
				if err = d.backend.MarkRead(d.ctx, room.ID, message.ID); err != nil {
					firstErr = errors.Join(firstErr, err)
				}
				break
			}
		}
		if firstErr != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(firstErr)
				}
			})
		}
	}()
}

func (d *DesktopApp) setUnreadTrayState(rooms []matrix.Room) {
	if d.trayApp == nil {
		return
	}
	unread := 0
	for _, room := range rooms {
		unread += room.Unread
	}
	if unread > 0 {
		d.trayApp.SetSystemTrayIcon(unreadIconResource())
	} else {
		d.trayApp.SetSystemTrayIcon(iconResource())
	}
}

func (d *DesktopApp) Run() {
	go d.listenForBackendEvents()
	if d.backend.IsLoggedIn() {
		d.showStarting()
		go func() {
			err := d.backend.Start(d.ctx)
			fyne.Do(func() {
				if d.ctx.Err() != nil {
					return
				}
				if err != nil {
					d.showError(err)
					return
				}
				if d.backend.NeedsVerification() {
					d.showVerification()
				} else {
					d.showMain()
				}
			})
		}()
	} else {
		d.showLogin()
	}
	d.window.ShowAndRun()
	d.cancel()
	d.backend.Close()
}

func (d *DesktopApp) showStarting() {
	d.window.SetContent(container.NewCenter(container.NewVBox(
		widget.NewLabelWithStyle("Starting Matrix", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewProgressBarInfinite(),
	)))
}

func (d *DesktopApp) listenForBackendEvents() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case update := <-d.backend.Events():
			// gomuks calls its event handler from sync/decryption goroutines.
			// All widget and window mutations are therefore marshalled to Fyne.
			fyne.Do(func() { d.applyUpdate(update) })
		}
	}
}

func (d *DesktopApp) showLogin() {
	homeserver := widget.NewEntry()
	homeserver.SetText("https://matrix.org")
	homeserver.SetPlaceHolder("https://matrix.example.org")
	username := widget.NewEntry()
	username.SetPlaceHolder("@alice:matrix.example.org or alice")
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("Password")
	status := widget.NewLabel("")
	login := widget.NewButton("Log in", nil)
	login.OnTapped = func() {
		server := strings.TrimSpace(homeserver.Text)
		user := strings.TrimSpace(username.Text)
		if server == "" || user == "" || password.Text == "" {
			status.SetText("Enter your homeserver, username, and password to continue.")
			return
		}
		login.Disable()
		status.SetText("Connecting securely to your homeserver...")
		go func(server, user, pass string) {
			err := d.backend.Login(d.ctx, server, user, pass)
			fyne.Do(func() {
				if d.ctx.Err() != nil {
					return
				}
				login.Enable()
				password.SetText("")
				if err != nil {
					if d.backend.IsLoggedIn() {
						if d.backend.NeedsVerification() {
							d.showVerification()
						} else {
							d.showMain()
							d.chat.SetStatus(err.Error())
						}
						return
					}
					status.SetText(fmt.Sprintf("Couldn't sign in: %v", err))
					return
				}
				if d.backend.NeedsVerification() {
					d.showVerification()
				} else {
					d.showMain()
				}
			})
		}(server, user, password.Text)
	}
	homeserverField := loginField(
		"Homeserver",
		"Your homeserver is the service that hosts your Matrix account. Use the URL provided by your organization, or keep matrix.org for a matrix.org account.",
		homeserver,
	)
	usernameField := loginField(
		"Username",
		"Enter your full Matrix ID (for example, @alice:matrix.org) or your username if your homeserver accepts it.",
		username,
	)
	passwordField := loginField(
		"Password",
		"Your password is used to authenticate with the homeserver you selected. This sign-in form does not save it.",
		password,
	)
	login.Importance = widget.HighImportance
	panel := widget.NewCard("", "", container.NewVBox(
		widget.NewLabelWithStyle("Welcome to Matrix", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Sign in with the account you use in any Matrix client."),
		widget.NewSeparator(),
		homeserverField,
		usernameField,
		passwordField,
		login,
		status,
		widget.NewSeparator(),
		widget.NewLabel("This app keeps your session and message history on this device so you can pick up where you left off. Protect your device account to keep that data private."),
	))
	card := container.NewGridWrap(fyne.NewSize(560, 620), panel)
	d.window.SetContent(container.NewCenter(card))
}

func loginField(label, explanation string, input *widget.Entry) fyne.CanvasObject {
	explainer := widget.NewLabel(explanation)
	explainer.Wrapping = fyne.TextWrapWord
	return container.NewVBox(
		widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		explainer,
		container.NewGridWrap(fyne.NewSize(520, 40), input),
	)
}

func (d *DesktopApp) showVerification() {
	recoveryKey := widget.NewPasswordEntry()
	recoveryKey.SetPlaceHolder("Recovery key or Secure Backup passphrase")
	status := widget.NewLabel("Verify this device to restore encryption keys and start syncing.")
	verify := widget.NewButton("Verify and continue", nil)
	verify.OnTapped = func() {
		if recoveryKey.Text == "" {
			status.SetText("Enter your recovery key or Secure Backup passphrase.")
			return
		}
		verify.Disable()
		status.SetText("Verifying encryption keys...")
		go func(code string) {
			err := d.backend.Verify(d.ctx, code)
			fyne.Do(func() {
				if d.ctx.Err() != nil {
					return
				}
				verify.Enable()
				if err != nil {
					status.SetText(err.Error())
					return
				}
				d.showMain()
			})
		}(recoveryKey.Text)
	}
	d.window.SetContent(container.NewCenter(container.NewVBox(
		widget.NewLabelWithStyle("Verify this device", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Your Matrix server requires key verification before syncing."),
		recoveryKey,
		verify,
		status,
	)))
}

func (d *DesktopApp) showMain() {
	d.chat = NewChatView(d.sendMessage)
	d.chat.SetAvatarCache(d.avatars)
	d.chat.SetSendOnCtrlEnter(d.fyneApp.Preferences().Bool("composer.ctrl_enter"))
	d.chat.SetShowAvatars(d.fyneApp.Preferences().BoolWithFallback("appearance.show_avatars", true))
	d.chat.Set24HourTime(d.fyneApp.Preferences().Bool("appearance.time_24_hour"))
	d.chat.SetLoadOlderHandler(d.loadOlderMessages)
	d.chat.SetRetryHandler(d.retryMessage)
	d.chat.SetTypingHandler(d.sendTyping)
	d.chat.SetReplyHandler(d.sendReply, d.chat.selectReply)
	d.chat.SetReactionHandler(d.reactToMessage)
	d.chat.SetEditHandler(d.editMessage)
	d.chat.SetRedactHandler(d.confirmRedact)
	d.chat.SetSearchHandler(d.searchMessages)
	d.chat.SetLeaveHandler(d.confirmLeaveRoom)
	d.chat.SetAttachmentHandler(d.chooseAttachment)
	d.chat.SetEmojiHandler(d.showEmojiPicker)
	d.chat.SetDownloadHandler(d.downloadMedia)
	d.chat.SetMentionSendHandler(d.sendMessageWithMentions)
	d.chat.SetRoomPreferenceHandlers(d.toggleFavorite, d.toggleMute)
	d.roomList = NewRoomList(d.selectRoom)
	d.roomList.SetAvatarCache(d.avatars)
	d.roomList.SetActions(d.toggleTheme, d.confirmLogout)
	d.roomList.SetSettingsAction(d.showSettings)
	d.roomList.SetTriageActions(d.markAnswered, d.undoAnswered)
	d.syncReady = d.backend.SyncReady()
	rooms := d.decorateRooms(d.backend.Rooms())
	d.roomList.SetRooms(rooms)
	d.setUnreadTrayState(rooms)
	d.roomList.SetSyncReady(d.syncReady)
	d.chat.SetSyncReady(d.syncReady)
	split := container.NewHSplit(d.roomList.Object(), d.chat.Object())
	split.Offset = 0.28
	d.window.SetContent(split)
	d.installShortcuts()
	if len(rooms) > 0 {
		d.selectRoom(rooms[0])
	}
}

func (d *DesktopApp) applyUpdate(update matrix.Update) {
	d.syncReady = d.syncReady || update.InitialSyncComplete
	if d.roomList == nil {
		return
	}
	d.roomList.SetSyncReady(d.syncReady)
	d.chat.SetSyncReady(d.syncReady)
	status := update.Error
	if status == "" {
		switch update.ConnectionState {
		case matrix.StateConnecting:
			status = "Connecting to the homeserver…"
		case matrix.StateSyncing:
			status = "Syncing…"
		case matrix.StateConnected:
			status = "Connected"
		case matrix.StateOffline:
			status = "Offline"
		}
	}
	d.chat.SetStatus(status)
	d.chat.SetTypingStatus(update.TypingUsers[d.selectedRoom])
	for _, notification := range update.Notifications {
		if !d.fyneApp.Preferences().BoolWithFallback("notifications.enabled", true) {
			continue
		}
		if d.fyneApp.Preferences().Bool("room.muted."+notification.RoomID) || notification.RoomID == d.selectedRoom {
			continue
		}
		privacy := d.fyneApp.Preferences().StringWithFallback("notifications.privacy", "Sender and message preview")
		title := notification.Room
		if room := d.findRoom(update.Rooms, notification.RoomID); title == "" && room != nil {
			title = room.Name
		}
		if notification.Sender != "" {
			title = notification.Sender + " · " + title
		}
		body := notification.Body
		switch privacy {
		case "Sender only":
			body = "New message"
		case "Hidden content":
			title = "Matrix"
			body = "New activity"
		}
		d.fyneApp.SendNotification(fyne.NewNotification(title, body))
	}
	if update.Error != "" {
		d.showError(errors.New(update.Error))
	} else {
		d.window.SetTitle("Matrix")
	}
	rooms := d.decorateRooms(update.Rooms)
	d.roomList.SetRooms(rooms)
	d.setUnreadTrayState(rooms)
	timelineChanged := make(map[string]struct{}, len(update.TimelineChangedRoomID))
	for _, roomID := range update.TimelineChangedRoomID {
		timelineChanged[roomID] = struct{}{}
	}
	for _, roomID := range update.ChangedRoomID {
		if roomID == d.selectedRoom {
			for _, room := range rooms {
				if room.ID == roomID {
					d.chat.UpdateRoom(room)
					break
				}
			}
			if d.findRoom(rooms, roomID) == nil {
				d.selectedRoom = ""
				d.chat.SetRoom(matrix.Room{})
			}
		}
	}
	if _, changed := timelineChanged[d.selectedRoom]; changed {
		d.reloadMessages(d.selectedRoom)
	}
	if d.selectedRoom == "" && len(rooms) > 0 {
		d.selectRoom(rooms[0])
	}
}

func (d *DesktopApp) selectRoom(room matrix.Room) {
	d.selectedRoom = room.ID
	d.roomList.SelectRoom(room.ID)
	d.chat.SetRoom(room)
	d.reloadMessages(room.ID)
	go func(roomID string) {
		members, err := d.backend.RoomMembers(d.ctx, roomID)
		fyne.Do(func() {
			if d.ctx.Err() != nil || d.selectedRoom != roomID || d.chat == nil {
				return
			}
			if err != nil {
				d.showError(err)
				return
			}
			d.chat.SetMembers(members)
		})
	}(room.ID)
}

func (d *DesktopApp) reloadMessages(roomID string) {
	d.loadSequence++
	sequence := d.loadSequence
	d.chat.SetMessagesLoading(true)
	go func() {
		page, err := d.backend.HistoryPage(d.ctx, roomID, 0)
		fyne.Do(func() {
			if d.ctx.Err() != nil || sequence != d.loadSequence || roomID != d.selectedRoom || d.chat == nil {
				return
			}
			if err != nil {
				d.chat.SetMessagesLoading(false)
				d.showError(err)
				return
			}
			d.chat.RefreshLatest(page)
			d.markRead(roomID, page)
		})
	}()
}

func (d *DesktopApp) loadOlderMessages(roomID string, before int64) {
	go func() {
		page, err := d.backend.HistoryPage(d.ctx, roomID, before)
		fyne.Do(func() {
			if d.ctx.Err() != nil || roomID != d.selectedRoom || d.chat == nil {
				return
			}
			if err != nil {
				d.chat.OlderLoadFailed(err.Error())
				return
			}
			d.chat.SetHistoryPage(page, true)
		})
	}()
}

func (d *DesktopApp) markRead(roomID string, page matrix.HistoryPage) {
	if len(page.Messages) == 0 {
		return
	}
	eventID := ""
	for i := len(page.Messages) - 1; i >= 0; i-- {
		if page.Messages[i].ID != "" && !strings.HasPrefix(page.Messages[i].ID, "~") {
			eventID = page.Messages[i].ID
			break
		}
	}
	if eventID == "" {
		return
	}
	go func() {
		if err := d.backend.MarkRead(d.ctx, roomID, eventID); err != nil && d.ctx.Err() == nil {
			fyne.Do(func() { d.showError(err) })
		}
	}()
}

func (d *DesktopApp) sendMessage(roomID, text string) {
	go func() {
		if err := d.backend.SendMessage(d.ctx, roomID, text); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() { d.reloadMessages(roomID) })
	}()
}

func (d *DesktopApp) retryMessage(roomID, transactionID string) {
	go func() {
		if err := d.backend.RetryMessage(d.ctx, transactionID); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() { d.reloadMessages(roomID) })
	}()
}

func (d *DesktopApp) sendReply(roomID, text, replyTo string) {
	go func() {
		if err := d.backend.SendReply(d.ctx, roomID, text, replyTo); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() { d.reloadMessages(roomID) })
	}()
}

func (d *DesktopApp) reactToMessage(message matrix.Message, emoji string) {
	roomID := d.selectedRoom
	go func() {
		if err := d.backend.React(d.ctx, roomID, message.ID, emoji); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
		}
	}()
}

func (d *DesktopApp) editMessage(message matrix.Message) {
	roomID := d.selectedRoom
	editor := dialog.NewEntryDialog("Edit message", "Update the message text:", func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		go func() {
			if err := d.backend.EditMessage(d.ctx, roomID, text, message.ID); err != nil {
				fyne.Do(func() { d.showError(err) })
			}
		}()
	}, d.window)
	editor.SetText(message.Body)
	editor.Show()
}

func (d *DesktopApp) confirmRedact(message matrix.Message) {
	roomID := d.selectedRoom
	dialog.ShowConfirm("Delete message?", "This will redact your message for everyone in the room.", func(confirm bool) {
		if !confirm {
			return
		}
		go func() {
			if err := d.backend.RedactMessage(d.ctx, roomID, message.ID); err != nil {
				fyne.Do(func() { d.showError(err) })
			}
		}()
	}, d.window)
}

func (d *DesktopApp) chooseAttachment(roomID string) {
	chooser := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, d.window)
			return
		}
		if reader == nil {
			return
		}
		d.uploadAttachment(roomID, reader.URI().Name(), reader)
	}, d.window)
	chooser.Show()
}

func (d *DesktopApp) downloadMedia(message matrix.Message) {
	go func() {
		data, err := d.backend.DownloadMedia(d.ctx, message.MediaURI, message.MediaEncrypted)
		if err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			if strings.HasPrefix(message.MediaMIME, "image/") {
				image := canvas.NewImageFromResource(fyne.NewStaticResource("matrix-image", data))
				image.FillMode = canvas.ImageFillContain
				image.SetMinSize(fyne.NewSize(640, 480))
				viewer := dialog.NewCustomConfirm("Image preview", "Save image", "Close", image, func(save bool) {
					if save {
						d.showMediaSaveDialog(data, message.MediaMIME)
					}
				}, d.window)
				viewer.Resize(fyne.NewSize(720, 560))
				viewer.Show()
				return
			}
			d.showMediaSaveDialog(data, message.MediaMIME)
		})
	}()
}

func (d *DesktopApp) showMediaSaveDialog(data []byte, mediaMIME string) {
	filename := "matrix-media"
	if extensions, err := mime.ExtensionsByType(mediaMIME); err == nil && len(extensions) > 0 {
		filename += extensions[0]
	}
	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, saveErr error) {
		if saveErr != nil {
			d.showError(saveErr)
			return
		}
		if writer == nil {
			return
		}
		go func() {
			defer writer.Close()
			if _, writeErr := writer.Write(data); writeErr != nil {
				fyne.Do(func() {
					if d.ctx.Err() == nil {
						d.showError(writeErr)
					}
				})
			}
		}()
	}, d.window)
	saveDialog.SetFileName(filename)
	saveDialog.Show()
}

func (d *DesktopApp) uploadAttachment(roomID, name string, reader io.ReadCloser) {
	go func() {
		defer reader.Close()
		const maxAttachmentBytes = 50 << 20
		data, err := io.ReadAll(io.LimitReader(reader, maxAttachmentBytes+1))
		if err == nil && len(data) > maxAttachmentBytes {
			err = fmt.Errorf("file exceeds %d MiB", maxAttachmentBytes>>20)
		}
		if err == nil {
			err = d.backend.SendAttachment(d.ctx, roomID, name, data)
		}
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			if err != nil {
				d.showError(err)
				return
			}
			if roomID == d.selectedRoom {
				d.reloadMessages(roomID)
			}
		})
	}()
}

func (d *DesktopApp) showEmojiPicker() {
	emojis := []string{"😀", "😂", "🥰", "👍", "👎", "❤️", "🎉", "🙏", "👀", "🔥", "✅", "🤔", "😢", "🙌", "🚀"}
	buttons := make([]fyne.CanvasObject, 0, len(emojis))
	var picker dialog.Dialog
	for _, emoji := range emojis {
		choice := emoji
		buttons = append(buttons, widget.NewButton(choice, func() {
			d.chat.input.SetText(d.chat.input.Text + choice)
			picker.Hide()
			d.window.Canvas().Focus(d.chat.input.Entry)
		}))
	}
	picker = dialog.NewCustom("Choose an emoji", "Close", container.NewGridWithColumns(5, buttons...), d.window)
	picker.Resize(fyne.NewSize(340, 220))
	picker.Show()
}

func (d *DesktopApp) sendTyping(roomID, text string) {
	timeout := 5 * time.Second
	if strings.TrimSpace(text) == "" {
		timeout = 0
	}
	go func() {
		if err := d.backend.SetTyping(d.ctx, roomID, timeout); err != nil && d.ctx.Err() == nil {
			fyne.Do(func() { d.showError(err) })
		}
	}()
}

func (d *DesktopApp) searchMessages(roomID string) {
	query := widget.NewEntry()
	query.SetPlaceHolder("Search messages in this room")
	status := widget.NewLabel("")
	results := widget.NewList(
		func() int { return 0 },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(widget.ListItemID, fyne.CanvasObject) {},
	)
	searchDialog := dialog.NewCustom("Search in chat", "Close", container.NewBorder(query, status, nil, nil, results), d.window)
	searchDialog.Resize(fyne.NewSize(640, 420))
	var matches []matrix.Message
	results.Length = func() int { return len(matches) }
	results.UpdateItem = func(id widget.ListItemID, object fyne.CanvasObject) {
		if id < 0 || id >= len(matches) {
			return
		}
		message := matches[id]
		label := object.(*widget.Label)
		sender := message.SenderName
		if sender == "" {
			sender = message.Sender
		}
		label.SetText(fmt.Sprintf("%s  %s\n%s", sender, message.Timestamp.Local().Format(time.DateTime), message.Body))
		label.Wrapping = fyne.TextWrapWord
	}
	query.OnSubmitted = func(value string) {
		term := strings.TrimSpace(value)
		if term == "" {
			status.SetText("Enter a word or phrase to search.")
			return
		}
		status.SetText("Searching local message history...")
		go func() {
			found, err := d.backend.SearchMessages(d.ctx, roomID, term, 50)
			fyne.Do(func() {
				if d.ctx.Err() != nil {
					return
				}
				if err != nil {
					status.SetText(err.Error())
					return
				}
				matches = found
				results.Refresh()
				if len(matches) == 0 {
					status.SetText("No matching messages found in the local history.")
				} else {
					status.SetText(fmt.Sprintf("%d matching messages", len(matches)))
				}
			})
		}()
	}
	searchDialog.Show()
	d.window.Canvas().Focus(query)
}

func (d *DesktopApp) confirmLeaveRoom(roomID string) {
	dialog.ShowConfirm("Leave chat?", "You will leave this Matrix room and stop receiving its messages.", func(confirm bool) {
		if !confirm {
			return
		}
		go func() {
			if err := d.backend.LeaveRoom(d.ctx, roomID); err != nil {
				fyne.Do(func() { d.showError(err) })
			}
		}()
	}, d.window)
}

func (d *DesktopApp) confirmLogout() {
	dialog.ShowConfirm("Log out of Matrix?", "This terminates the server session and removes this app's local message history and encryption keys from this device.", func(confirm bool) {
		if !confirm {
			return
		}
		go func() {
			err := d.backend.Logout(d.ctx)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, d.window)
					return
				}
				d.cancel()
				d.fyneApp.Quit()
			})
		}()
	}, d.window)
}

func (d *DesktopApp) decorateRooms(rooms []matrix.Room) []matrix.Room {
	preferences := d.fyneApp.Preferences()
	for i := range rooms {
		rooms[i].Favorite = preferences.Bool("room.favorite." + rooms[i].ID)
		rooms[i].Muted = preferences.Bool("room.muted." + rooms[i].ID)
	}
	return rooms
}

func (d *DesktopApp) toggleFavorite(roomID string) {
	key := "room.favorite." + roomID
	value := !d.fyneApp.Preferences().Bool(key)
	d.fyneApp.Preferences().SetBool(key, value)
	d.refreshRoomPreferences(roomID)
}

func (d *DesktopApp) toggleMute(roomID string) {
	key := "room.muted." + roomID
	value := !d.fyneApp.Preferences().Bool(key)
	d.fyneApp.Preferences().SetBool(key, value)
	d.refreshRoomPreferences(roomID)
}

func (d *DesktopApp) refreshRoomPreferences(roomID string) {
	rooms := d.decorateRooms(d.backend.Rooms())
	d.roomList.SetRooms(rooms)
	if room := d.findRoom(rooms, roomID); room != nil && roomID == d.selectedRoom {
		d.chat.UpdateRoom(*room)
	}
}

func (d *DesktopApp) toggleTheme() {
	choice := "Dark"
	if d.themeDark {
		choice = "Light"
	}
	d.fyneApp.Preferences().SetString("appearance.theme", choice)
	d.applyTheme(choice)
}

func (d *DesktopApp) installShortcuts() {
	canvas := d.window.Canvas()
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
		if d.fyneApp.Preferences().Bool("composer.ctrl_enter") && canvas.Focused() == d.chat.input.Entry {
			d.chat.submit()
		}
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierSuper}, func(fyne.Shortcut) {
		if d.fyneApp.Preferences().Bool("composer.ctrl_enter") && canvas.Focused() == d.chat.input.Entry {
			d.chat.submit()
		}
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyE}, func(fyne.Shortcut) {
		if d.selectedRoom != "" && canvas.Focused() != d.chat.input.Entry {
			d.markAnswered(d.selectedRoom)
		}
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
		if canvas.Focused() != d.chat.input.Entry {
			d.undoAnswered()
		}
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierSuper}, func(fyne.Shortcut) {
		if canvas.Focused() != d.chat.input.Entry {
			d.undoAnswered()
		}
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyK, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
		d.showQuickSwitcher()
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyK, Modifier: fyne.KeyModifierSuper}, func(fyne.Shortcut) {
		d.showQuickSwitcher()
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierControl}, func(fyne.Shortcut) {
		if d.selectedRoom != "" {
			d.searchMessages(d.selectedRoom)
		}
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: fyne.KeyModifierShortcutDefault}, func(fyne.Shortcut) {
		d.showSettings()
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyUp, Modifier: fyne.KeyModifierAlt}, func(fyne.Shortcut) {
		d.cycleRoom(-1)
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyDown, Modifier: fyne.KeyModifierAlt}, func(fyne.Shortcut) {
		d.cycleRoom(1)
	})
	canvas.AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyEscape}, func(fyne.Shortcut) {
		if d.quickSwitcher != nil {
			d.quickSwitcher.Hide()
			d.quickSwitcher = nil
		} else {
			canvas.Unfocus()
		}
	})
}

func (d *DesktopApp) markAnswered(roomID string) {
	if roomID == "" {
		return
	}
	nextRoomID := d.roomList.NextVisibleRoom(roomID)
	go func() {
		if err := d.backend.SetRoomUnanswered(d.ctx, roomID, false); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			d.lastAnsweredRoom = roomID
			d.roomList.SetUndoVisible(true)
			if nextRoomID != "" {
				if room := d.findRoom(d.decorateRooms(d.backend.Rooms()), nextRoomID); room != nil {
					d.selectRoom(*room)
				}
			}
		})
	}()
}

func (d *DesktopApp) undoAnswered() {
	roomID := d.lastAnsweredRoom
	if roomID == "" {
		return
	}
	go func() {
		if err := d.backend.SetRoomUnanswered(d.ctx, roomID, true); err != nil {
			fyne.Do(func() {
				if d.ctx.Err() == nil {
					d.showError(err)
				}
			})
			return
		}
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			d.lastAnsweredRoom = ""
			d.roomList.SetUndoVisible(false)
			if room := d.findRoom(d.decorateRooms(d.backend.Rooms()), roomID); room != nil {
				d.selectRoom(*room)
			}
		})
	}()
}

func (d *DesktopApp) showQuickSwitcher() {
	rooms := d.backend.Rooms()
	if len(rooms) == 0 {
		return
	}
	var visible []matrix.Room
	var quickDialog dialog.Dialog
	selected := widget.ListItemID(-1)
	list := widget.NewList(
		func() int { return len(visible) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id >= 0 && id < len(visible) {
				object.(*widget.Label).SetText(visible[id].Name)
			}
		},
	)
	openRoom := func(index widget.ListItemID) {
		if index >= 0 && index < len(visible) {
			d.selectRoom(visible[index])
			quickDialog.Hide()
			d.quickSwitcher = nil
		}
	}
	closeDialog := func() {
		quickDialog.Hide()
		d.quickSwitcher = nil
	}
	list.OnSelected = func(index widget.ListItemID) {
		selected = index
	}
	refresh := func(term string) {
		visible = visible[:0]
		term = strings.TrimSpace(term)
		type rankedRoom struct {
			room  matrix.Room
			score int
		}
		ranked := make([]rankedRoom, 0, len(rooms))
		for _, room := range rooms {
			if term == "" {
				ranked = append(ranked, rankedRoom{room: room})
				continue
			}
			nameScore, nameMatch := fuzzyScore(term, room.Name)
			idScore, idMatch := fuzzyScore(term, room.ID)
			if nameMatch || idMatch {
				score := nameScore
				if idMatch && (!nameMatch || idScore > score) {
					score = idScore
				}
				ranked = append(ranked, rankedRoom{room: room, score: score})
			}
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			return ranked[i].score > ranked[j].score
		})
		for _, result := range ranked {
			visible = append(visible, result.room)
		}
		list.Refresh()
		if len(visible) > 0 {
			selected = 0
			list.Select(0)
		} else {
			selected = -1
		}
	}
	search := &quickSwitcherEntry{
		Entry: widget.NewEntry(),
		onMove: func(direction int) {
			if len(visible) == 0 {
				return
			}
			selected = widget.ListItemID((int(selected) + direction + len(visible)) % len(visible))
			list.Select(selected)
		},
		onOpen:  func() { openRoom(selected) },
		onClose: closeDialog,
	}
	search.SetPlaceHolder("Type a chat name or Matrix ID")
	search.OnChanged = refresh
	refresh("")
	footer := container.NewHBox(widget.NewButton("Close", closeDialog), widget.NewButton("Open selected chat", func() { openRoom(selected) }))
	content := container.NewBorder(search, footer, nil, nil, list)
	quickDialog = dialog.NewCustomWithoutButtons("Quick switcher", content, d.window)
	quickDialog.Resize(fyne.NewSize(480, 360))
	quickDialog.Show()
	d.quickSwitcher = quickDialog
	d.window.Canvas().Focus(search)
}

func fuzzyScore(query, target string) (int, bool) {
	queryRunes := []rune(strings.ToLower(query))
	targetRunes := []rune(strings.ToLower(target))
	if len(queryRunes) == 0 {
		return 0, true
	}
	queryIndex := 0
	score := 0
	lastIndex := -2
	for i, current := range targetRunes {
		if current != queryRunes[queryIndex] {
			continue
		}
		score += 10
		if i == 0 || targetRunes[i-1] == ' ' || targetRunes[i-1] == ':' || targetRunes[i-1] == '_' || targetRunes[i-1] == '-' {
			score += 8
		}
		if i == lastIndex+1 {
			score += 5
		}
		lastIndex = i
		queryIndex++
		if queryIndex == len(queryRunes) {
			score -= len(targetRunes) - len(queryRunes)
			return score, true
		}
	}
	return 0, false
}

type quickSwitcherEntry struct {
	*widget.Entry
	onMove  func(int)
	onOpen  func()
	onClose func()
}

func (e *quickSwitcherEntry) KeyDown(key *fyne.KeyEvent) {
	switch key.Name {
	case fyne.KeyUp:
		e.onMove(-1)
	case fyne.KeyDown:
		e.onMove(1)
	case fyne.KeyEscape:
		e.onClose()
	default:
		e.Entry.KeyDown(key)
	}
}

func (e *quickSwitcherEntry) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyReturn || key.Name == fyne.KeyEnter {
		e.onOpen()
		return
	}
	e.Entry.TypedKey(key)
}

func (d *DesktopApp) cycleRoom(direction int) {
	rooms := d.backend.Rooms()
	if len(rooms) < 2 {
		return
	}
	index := 0
	for i, room := range rooms {
		if room.ID == d.selectedRoom {
			index = (i + direction + len(rooms)) % len(rooms)
			break
		}
	}
	d.selectRoom(rooms[index])
}

func (d *DesktopApp) showError(err error) {
	if d.chat != nil {
		d.chat.SetStatus(err.Error())
	}
	d.window.SetTitle(fmt.Sprintf("Matrix - %v", err))
}

func (d *DesktopApp) findRoom(rooms []matrix.Room, roomID string) *matrix.Room {
	for i := range rooms {
		if rooms[i].ID == roomID {
			return &rooms[i]
		}
	}
	return nil
}
