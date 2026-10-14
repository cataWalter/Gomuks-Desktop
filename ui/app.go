package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"mychat/matrix"
)

type DesktopApp struct {
	backend *matrix.Client
	window  fyne.Window

	ctx          context.Context
	cancel       context.CancelFunc
	roomList     *RoomList
	chat         *ChatView
	selectedRoom string
	loadSequence uint64
	syncReady    bool
	shutdownOnce sync.Once
}

func NewApp(backend *matrix.Client) *DesktopApp {
	fyneApp := app.NewWithID("fi.mau.fyne-matrix")
	window := fyneApp.NewWindow("Matrix")
	ctx, cancel := context.WithCancel(context.Background())
	desktop := &DesktopApp{
		backend: backend,
		window:  window,
		ctx:     ctx,
		cancel:  cancel,
	}
	window.Resize(fyne.NewSize(1100, 720))
	window.SetCloseIntercept(func() {
		desktop.shutdownOnce.Do(func() {
			desktop.cancel()
			go func() {
				desktop.backend.Close()
				fyne.Do(fyneApp.Quit)
			}()
		})
	})
	return desktop
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
	d.chat.SetLoadOlderHandler(d.loadOlderMessages)
	d.roomList = NewRoomList(d.selectRoom)
	d.syncReady = d.backend.SyncReady()
	rooms := d.backend.Rooms()
	d.roomList.SetRooms(rooms)
	d.roomList.SetSyncReady(d.syncReady)
	d.chat.SetSyncReady(d.syncReady)
	split := container.NewHSplit(d.roomList.Object(), d.chat.Object())
	split.Offset = 0.28
	d.window.SetContent(split)
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
	d.chat.SetStatus(update.Error)
	if update.Error != "" {
		d.showError(errors.New(update.Error))
	} else {
		d.window.SetTitle("Matrix")
	}
	d.roomList.SetRooms(update.Rooms)
	for _, roomID := range update.ChangedRoomID {
		if roomID == d.selectedRoom {
			for _, room := range update.Rooms {
				if room.ID == roomID {
					d.chat.UpdateRoom(room)
					d.reloadMessages(roomID)
					break
				}
			}
			if d.findRoom(update.Rooms, roomID) == nil {
				d.selectedRoom = ""
				d.chat.SetRoom(matrix.Room{})
			}
		}
	}
	if d.selectedRoom == "" && len(update.Rooms) > 0 {
		d.selectRoom(update.Rooms[0])
	}
}

func (d *DesktopApp) selectRoom(room matrix.Room) {
	d.selectedRoom = room.ID
	d.roomList.SelectRoom(room.ID)
	d.chat.SetRoom(room)
	d.reloadMessages(room.ID)
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
