package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mychat/matrix"
	"mychat/notifications"
)

type Focus int

const (
	FocusRooms Focus = iota
	FocusTimeline
	FocusComposer
)

type Model struct {
	client        *matrix.Client
	rooms         roomsModel
	timeline      timelineModel
	composer      composerModel
	login         loginModel
	help          helpModel
	focus         Focus
	showHelp      bool
	loginActive   bool
	width         int
	height        int
	connection    matrix.ConnectionState
	roomsList     []matrix.Room
	currentRoom   *matrix.Room
	typing        map[string][]string
	errorMsg      string
	needsVerif    bool
	viewer        *imageViewer
	nativeImages  bool
	notice        string
	confirmDelete string
	notifier      notifications.Notifier
}

func NewModel(client *matrix.Client) *Model {
	return &Model{
		client:      client,
		rooms:       newRoomsModel(),
		timeline:    newTimelineModel(),
		composer:    newComposerModel(),
		login:       newLoginModel(),
		help:        newHelpModel(),
		focus:       FocusRooms,
		loginActive: client == nil || !client.IsLoggedIn(),
		showHelp:    false,
		connection:  matrix.StateOffline,
		typing:      make(map[string][]string),
		needsVerif:  false,
		notifier:    notifications.New(),
	}
}

type FocusMsg struct {
	Focus Focus
}

type MatrixUpdateMsg struct {
	Update matrix.Update
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	model := next.(Model)
	mediaCmd := model.mediaCmd()
	return model, tea.Batch(cmd, mediaCmd)
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var notificationCmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		var timelineCmd, composerCmd tea.Cmd
		m.timeline, timelineCmd = m.timeline.Update(msg)
		m.composer, composerCmd = m.composer.Update(msg)
		cmd := tea.Batch(timelineCmd, composerCmd)
		if m.viewer != nil {
			cmd = tea.Batch(cmd, tea.ClearScreen)
		}
		return m, cmd

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.viewer != nil {
			return m.handleViewerKey(msg)
		}
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "q" || msg.String() == "esc" {
				m.showHelp = false
				return m, nil
			}
			return m, nil
		}
		if m.loginActive {
			var cmd tea.Cmd
			m.login, cmd = m.login.Update(msg)
			return m, cmd
		}

		m.notice = ""
		pending := m.confirmDelete
		m.confirmDelete = ""
		switch msg.String() {
		case "?":
			if m.focus == FocusComposer {
				break
			}
			m.showHelp = !m.showHelp
			return m, nil
		case "q":
			if m.focus == FocusComposer {
				break
			}
			return m, tea.Quit
		case "1":
			if m.focus == FocusComposer {
				break
			}
			m.setFocus(FocusRooms)
			return m, nil
		case "2":
			if m.focus == FocusComposer {
				break
			}
			m.setFocus(FocusTimeline)
			return m, nil
		case "3":
			if m.focus == FocusComposer {
				break
			}
			m.setFocus(FocusComposer)
			return m, nil
		case "j", "down":
			if m.focus == FocusRooms {
				m.moveRoom(1)
				return m, nil
			}
			if m.focus == FocusTimeline {
				m.timeline.moveSelection(1)
				return m, nil
			}
		case "k", "up":
			if m.focus == FocusRooms {
				m.moveRoom(-1)
				return m, nil
			}
			if m.focus == FocusTimeline {
				if m.timeline.selected == 0 {
					cmd := m.loadOlderCmd()
					return m, cmd
				}
				m.timeline.moveSelection(-1)
				return m, nil
			}
		case "a", "u", "f", "n":
			if m.focus == FocusRooms {
				switch msg.String() {
				case "a":
					m.setRoomFilter(FilterAll)
				case "u":
					m.setRoomFilter(FilterUnread)
				case "f":
					m.setRoomFilter(FilterFavorite)
				case "n":
					m.setRoomFilter(FilterNeedsReply)
				}
				return m, nil
			}
		case "m":
			if m.currentRoom != nil && m.client != nil && m.focus != FocusComposer {
				return m, func() tea.Msg {
					err := m.client.MarkRead(context.Background(), m.currentRoom.ID, m.currentRoom.LastEventID)
					return ActionResultMsg{Err: err}
				}
			}
		case "r":
			if m.focus == FocusTimeline {
				if selected := m.timeline.selectedMessage(); selected != nil && selected.ID != "" {
					reply := *selected
					m.composer.mode = ModeReply
					m.composer.replyTo = &reply
					m.composer.editMsg = nil
					m.setFocus(FocusComposer)
					return m, nil
				}
			}
		case "e":
			if m.focus == FocusTimeline {
				if selected := m.timeline.selectedMessage(); selected != nil && selected.Own && selected.ID != "" {
					message := *selected
					m.composer.mode = ModeEdit
					m.composer.editMsg = &message
					m.composer.replyTo = nil
					m.composer.textarea.SetValue(message.Body)
					m.setFocus(FocusComposer)
					return m, nil
				}
			}
		case "o":
			if m.focus == FocusTimeline && m.client != nil {
				if selected := m.timeline.selectedMessage(); selected != nil && selected.MediaURI != "" {
					return m, openMediaCmd(m.client, *selected)
				}
			}
		case "v":
			if m.focus == FocusTimeline {
				if selected := m.timeline.selectedMessage(); selected != nil && posterURI(*selected) != "" {
					if !m.nativeImages {
						m.errorMsg = "native images need GOMUKS_TUI_IMAGES=1 and terminal.integrated.enableImages"
						return m, nil
					}
					if m.client != nil {
						m.viewer = &imageViewer{uri: posterURI(*selected), label: viewerLabel(*selected)}
						return m, loadViewerCmd(m.client, *selected)
					}
				}
			}
		case "esc":
			if m.focus == FocusComposer {
				if m.composer.mode != ModeNormal {
					m.cancelComposerMode()
					return m, nil
				}
				m.setFocus(FocusTimeline)
				return m, nil
			}
			m.errorMsg = ""
			return m, nil
		case "alt+enter", "ctrl+j":
			if m.focus == FocusComposer {
				m.composer.textarea.InsertString("\n")
				return m, nil
			}
		case "R":
			if m.focus == FocusTimeline && m.client != nil {
				if selected := m.timeline.selectedMessage(); selected != nil && selected.SendError != "" && selected.TransactionID != "" {
					client, transactionID := m.client, selected.TransactionID
					return m, func() tea.Msg {
						return ActionResultMsg{Err: client.RetryMessage(context.Background(), transactionID)}
					}
				}
			}
		case "d":
			if m.focus == FocusTimeline && m.client != nil && m.currentRoom != nil {
				if selected := m.timeline.selectedMessage(); selected != nil && selected.Own && selected.ID != "" && !selected.Redacted {
					if pending == selected.ID {
						client, roomID, eventID := m.client, m.currentRoom.ID, selected.ID
						return m, func() tea.Msg {
							return ActionResultMsg{Err: client.RedactMessage(context.Background(), roomID, eventID)}
						}
					}
					m.confirmDelete = selected.ID
					m.notice = "press d again to delete the selected message"
					return m, nil
				}
			}
		case "enter":
			if m.focus == FocusRooms {
				if room := m.rooms.selectedRoom(); room != nil {
					m.selectRoom(*room)
				}
				m.setFocus(FocusTimeline)
				return m, nil
			}
			if m.focus == FocusComposer {
				content := m.composer.textarea.Value()
				if strings.TrimSpace(content) != "" && m.currentRoom != nil && m.client != nil {
					roomID := m.currentRoom.ID
					replyToID := ""
					editID := ""
					if m.composer.replyTo != nil {
						replyToID = m.composer.replyTo.ID
					}
					if m.composer.editMsg != nil {
						editID = m.composer.editMsg.ID
					}
					cmd := func() tea.Msg {
						var err error
						if editID != "" {
							err = m.client.EditMessage(context.Background(), roomID, content, editID)
						} else if replyToID != "" {
							err = m.client.SendReply(context.Background(), roomID, content, replyToID)
						} else {
							err = m.client.SendMessage(context.Background(), roomID, content)
						}
						return ActionResultMsg{Err: err, SubmittedText: content}
					}
					return m, cmd
				}
			}
		}

	case LoginSubmitMsg:
		if m.client == nil {
			m.login.err = "Matrix client is unavailable."
			return m, nil
		}
		m.login.loading = true
		m.login.err = ""
		return m, func() tea.Msg {
			return LoginResultMsg{Err: m.client.Login(context.Background(), msg.Homeserver, msg.Username, msg.Password)}
		}

	case LoginResultMsg:
		m.login.loading = false
		if msg.Err != nil {
			m.login.err = msg.Err.Error()
			return m, nil
		}
		m.loginActive = false
		m.connection = m.client.ConnectionState()

	case ActionResultMsg:
		if msg.Err != nil {
			m.errorMsg = msg.Err.Error()
		} else {
			m.errorMsg = ""
			if msg.SubmittedText != "" && m.composer.textarea.Value() == msg.SubmittedText {
				m.composer.textarea.Reset()
				m.composer.mode = ModeNormal
				m.composer.replyTo = nil
				m.composer.editMsg = nil
			}
		}

	case viewerLoadedMsg:
		if m.viewer == nil || m.viewer.uri != msg.uri {
			return m, nil
		}
		if msg.err != nil {
			m.viewer = nil
			m.errorMsg = msg.err.Error()
			return m, nil
		}
		m.viewer = &imageViewer{uri: msg.uri, label: msg.label, payload: msg.payload}
		return m, nil

	case posterLoadedMsg:
		m.timeline.storePoster(msg)

	case olderPageMsg:
		m.timeline.loading = false
		if m.currentRoom == nil || msg.roomID != m.currentRoom.ID {
			return m, nil
		}
		if msg.err != nil {
			m.errorMsg = msg.err.Error()
			return m, nil
		}
		m.timeline.prependMessages(msg.messages, msg.hasMore)
		return m, nil

	case MatrixUpdateMsg:
		update := msg.Update
		if update.Rooms != nil {
			m.roomsList = update.Rooms
			m.updateRooms(update.Rooms)
		}
		if update.ConnectionState != "" {
			m.connection = update.ConnectionState
		}
		m.needsVerif = update.NeedsVerification
		if update.TypingUsers != nil {
			m.typing = update.TypingUsers
		}
		if update.Error != "" {
			m.errorMsg = update.Error
		} else if update.ConnectionState == matrix.StateConnected {
			m.errorMsg = ""
		}
		for _, rid := range update.TimelineChangedRoomID {
			if m.client != nil && m.currentRoom != nil && rid == m.currentRoom.ID {
				if pg, err := m.client.HistoryPage(context.Background(), rid, 0); err == nil {
					m.timeline.setMessages(pg.Messages, pg.HasMore)
				}
			}
		}
		notificationCmd = m.notificationCmd(update.Notifications)
	}

	var cmd tea.Cmd
	if _, ok := msg.(tea.KeyMsg); ok {
		if m.focus == FocusComposer {
			m.composer, cmd = m.composer.Update(msg)
		} else if m.focus == FocusTimeline {
			m.timeline, cmd = m.timeline.Update(msg)
		}
	}
	return m, tea.Batch(cmd, notificationCmd)
}

func (m Model) notificationCmd(incoming []matrix.Notification) tea.Cmd {
	if m.notifier == nil || len(incoming) == 0 {
		return nil
	}
	type pendingNotification struct {
		title  string
		body   string
		roomID string
	}
	var pending []pendingNotification
	for _, notification := range incoming {
		if notification.RoomID == "" || (m.currentRoom != nil && notification.RoomID == m.currentRoom.ID) {
			continue
		}
		roomName := notification.Room
		muted := false
		for _, room := range m.roomsList {
			if room.ID == notification.RoomID {
				if roomName == "" {
					roomName = room.Name
				}
				muted = room.Muted
				break
			}
		}
		if muted {
			continue
		}
		if roomName == "" {
			roomName = notification.RoomID
		}
		title := roomName
		if notification.Sender != "" {
			title = notification.Sender + " - " + roomName
		}
		body := notification.Body
		if body == "" {
			body = "New message"
		}
		pending = append(pending, pendingNotification{title: title, body: body, roomID: notification.RoomID})
	}
	if len(pending) == 0 {
		return nil
	}
	notifier := m.notifier
	return func() tea.Msg {
		if notifier.Permission() == "default" {
			permission, err := notifier.RequestPermission()
			if err != nil || permission != "granted" {
				return nil
			}
		}
		for _, notification := range pending {
			_ = notifier.Send(notification.title, notification.body, notification.roomID, false)
		}
		return nil
	}
}

func timeRef(t int64) time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(0, t*1e6)
}

func (m *Model) selectRoom(r matrix.Room) {
	selected := r
	m.currentRoom = &selected
	m.timeline.roomID = r.ID
	if m.client != nil {
		if pg, err := m.client.HistoryPage(context.Background(), r.ID, 0); err == nil {
			m.timeline.setMessages(pg.Messages, pg.HasMore)
			m.timeline.viewport.GotoBottom()
		}
	}
}

func (m *Model) setFocus(focus Focus) {
	m.focus = focus
	if focus == FocusComposer {
		m.composer.textarea.Focus()
	} else {
		m.composer.textarea.Blur()
	}
}

func (m Model) View() string {
	if m.loginActive {
		content := m.login.View()
		if m.width > 0 && m.height > 0 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
		}
		return content
	}
	if m.viewer != nil {
		return m.viewerView()
	}
	if m.showHelp {
		helpView := m.help.help.View(keys)
		return helpView
	}

	if m.needsVerif {
		return "Verification needed (recovery key). Complete login in desktop app or via matrix.Client verification flow."
	}

	g := m.layout()
	if g.compact {
		return m.compactView(g)
	}
	left := panel(sidebarStyle, g.rooms, m.borderFor(FocusRooms), m.rooms.View(g.rooms.inner().w))
	timeline := panel(timelineStyle, g.timeline, m.borderFor(FocusTimeline), m.timeline.View())
	composer := panel(composerStyle, g.composer, m.borderFor(FocusComposer), m.composer.View())
	right := lipgloss.JoinVertical(lipgloss.Left, m.headerBar(g.header), timeline, composer, m.footerBar(g.footer))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func panel(style lipgloss.Style, r rect, borderColor lipgloss.Color, content string) string {
	return style.Width(r.w - 2).Height(r.h - 2).BorderForeground(borderColor).Render(content)
}

func (m Model) borderFor(focus Focus) lipgloss.Color {
	if m.focus == focus {
		return accent
	}
	return border
}

func (m Model) headerBar(r rect) string {
	return headerStyle.Width(r.w).Render(fitLine(m.headerText(), r.w-2))
}

func (m Model) footerBar(r rect) string {
	return footerStyle.Width(r.w).Render(fitLine(m.statusText(), r.w-2))
}

func (m Model) compactView(g screenLayout) string {
	header := m.headerBar(g.header)
	footer := m.footerBar(g.footer)
	if m.focus == FocusRooms {
		body := panel(sidebarStyle, g.rooms, accent, m.rooms.View(g.rooms.inner().w))
		return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	}
	timeline := panel(timelineStyle, g.timeline, m.borderFor(FocusTimeline), m.timeline.View())
	composer := panel(composerStyle, g.composer, m.borderFor(FocusComposer), m.composer.View())
	return lipgloss.JoinVertical(lipgloss.Left, header, timeline, composer, footer)
}

func (m *Model) resize() {
	if m.width < 1 || m.height < 1 {
		return
	}
	g := m.layout()
	m.timeline.viewport.Width = g.timeline.inner().w
	m.timeline.viewport.Height = g.timeline.inner().h
	m.composer.textarea.SetWidth(g.composer.inner().w)
	m.rooms.rows = g.rooms.inner().h
	m.rooms.ensureVisible()
	m.timeline.viewport.SetContent(m.timeline.renderMessages())
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (m *Model) updateRooms(rooms []matrix.Room) {
	selectedID := ""
	if m.currentRoom != nil {
		selectedID = m.currentRoom.ID
	}
	m.rooms.rooms = append(m.rooms.rooms[:0], rooms...)
	filtered := m.rooms.filteredRooms()
	if len(filtered) == 0 {
		m.currentRoom = nil
		m.rooms.selected = 0
		m.rooms.ensureVisible()
		m.timeline.roomID = ""
		m.timeline.setMessages(nil, false)
		return
	}
	selected := -1
	for i := range filtered {
		if filtered[i].ID == selectedID {
			selected = i
			break
		}
	}
	if selected < 0 {
		selected = clampInt(m.rooms.selected, 0, len(filtered)-1)
	}
	m.rooms.selected = selected
	m.rooms.ensureVisible()
	room := filtered[selected]
	if room.ID == selectedID {
		m.currentRoom = &room
	} else {
		m.selectRoom(room)
	}
}

func (m *Model) moveRoom(delta int) {
	filtered := m.rooms.filteredRooms()
	if len(filtered) == 0 {
		return
	}
	selected := clampInt(m.rooms.selected+delta, 0, len(filtered)-1)
	if selected == m.rooms.selected && m.currentRoom != nil {
		return
	}
	m.rooms.selected = selected
	m.rooms.ensureVisible()
	m.selectRoom(filtered[selected])
}

func (m *Model) setRoomFilter(filter RoomFilter) {
	m.rooms.filter = filter
	m.updateRooms(m.rooms.rooms)
}

func (m Model) headerText() string {
	if m.currentRoom == nil {
		return "  | No room selected"
	}
	name := m.currentRoom.Name
	if name == "" {
		name = m.currentRoom.ID
	}
	text := "  | " + name + "  | " + string(m.connection)
	if typing := m.typingText(); typing != "" {
		text += "  | " + typing
	}
	return text
}

func (m Model) statusText() string {
	if m.errorMsg != "" {
		return "ERR: " + m.errorMsg
	}
	if m.notice != "" {
		return m.notice
	}
	if m.focus == FocusComposer {
		return m.composerHint()
	}
	filter := "all"
	_ = filter
	switch m.rooms.filter {
	case FilterUnread:
		filter = "unread"
	case FilterFavorite:
		filter = "favorites"
	case FilterNeedsReply:
		filter = "needs-reply"
	}
	return fmt.Sprintf("%s (%d) | a all  u unread  f favorites  n needs reply | 1 rooms  2 timeline  3 compose  ? help", filter, len(m.rooms.filteredRooms()))
}

type olderPageMsg struct {
	roomID   string
	messages []matrix.Message
	hasMore  bool
	err      error
}

func (m *Model) loadOlderCmd() tea.Cmd {
	if m.client == nil || m.currentRoom == nil || m.timeline.loading || !m.timeline.hasMore {
		return nil
	}
	before := m.timeline.oldestCursor()
	if before == 0 {
		return nil
	}
	m.timeline.loading = true
	client, roomID := m.client, m.currentRoom.ID
	return func() tea.Msg {
		page, err := client.HistoryPage(context.Background(), roomID, before)
		return olderPageMsg{roomID: roomID, messages: page.Messages, hasMore: page.HasMore, err: err}
	}
}

func (m *Model) cancelComposerMode() {
	if m.composer.mode == ModeEdit {
		m.composer.textarea.Reset()
	}
	m.composer.mode = ModeNormal
	m.composer.replyTo = nil
	m.composer.editMsg = nil
}

func (m Model) composerHint() string {
	switch m.composer.mode {
	case ModeReply:
		return "replying to " + replySenderName(m.composer.replyTo) + " · enter send · esc cancel"
	case ModeEdit:
		return "editing · enter save · esc cancel"
	}
	return "enter send · alt+enter newline · esc timeline"
}

func replySenderName(msg *matrix.Message) string {
	if msg == nil {
		return ""
	}
	if msg.SenderName != "" {
		return msg.SenderName
	}
	return msg.Sender
}

func (m Model) typingText() string {
	users := m.typing[m.currentRoom.ID]
	switch len(users) {
	case 0:
		return ""
	case 1:
		return users[0] + " is typing…"
	default:
		return fmt.Sprintf("%d people typing…", len(users))
	}
}

type ActionResultMsg struct {
	Err           error
	SubmittedText string
}
