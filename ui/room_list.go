package ui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"mychat/matrix"
)

type RoomList struct {
	list        *widget.List
	empty       *widget.Label
	rooms       []matrix.Room
	visible     []matrix.Room
	avatars     *avatarCache
	avatarViews map[*fyne.Container]*avatarView
	controls    fyne.CanvasObject
	actions     *fyne.Container
	onSelect    func(matrix.Room)
	selected    string
	query       string
	filter      roomFilter
	syncReady   bool
	unanswered  *widget.Button
	onAnswered  func(roomID string)
	onUndo      func()
	undoVisible bool
	onTheme     func()
	onSettings  func()
	onLogout    func()
}

func (r *RoomList) SetTriageActions(answered func(roomID string), undo func()) {
	r.onAnswered = answered
	r.onUndo = undo
	r.actions.Objects[0].(*widget.Button).OnTapped = func() {
		if r.selected != "" && r.onAnswered != nil {
			r.onAnswered(r.selected)
		}
	}
	r.SetUndoVisible(false)
}

func (r *RoomList) SetUndoVisible(visible bool) {
	r.undoVisible = visible
	if visible && r.onUndo != nil {
		r.actions.Objects[1].Show()
	} else {
		r.actions.Objects[1].Hide()
	}
}

func (r *RoomList) NextVisibleRoom(roomID string) string {
	for i, room := range r.visible {
		if room.ID == roomID && len(r.visible) > 1 {
			return r.visible[(i+1)%len(r.visible)].ID
		}
	}
	for _, room := range r.rooms {
		if room.ID != roomID {
			return room.ID
		}
	}
	return ""
}

type roomFilter int

const (
	allRooms roomFilter = iota
	unansweredRooms
	directRooms
	groupRooms
)

func NewRoomList(onSelect func(matrix.Room)) *RoomList {
	rooms := &RoomList{
		onSelect:    onSelect,
		empty:       widget.NewLabel("Syncing your chats from the homeserver. Large accounts may take a little while."),
		avatarViews: make(map[*fyne.Container]*avatarView),
	}
	rooms.empty.Wrapping = fyne.TextWrapWord
	search := widget.NewEntry()
	search.SetPlaceHolder("Search chats")
	search.OnChanged = func(text string) {
		rooms.query = strings.TrimSpace(text)
		rooms.refresh()
	}
	rooms.list = widget.NewList(
		func() int { return len(rooms.visible) },
		func() fyne.CanvasObject {
			avatar := newAvatarView(42)
			rooms.avatarViews[avatar.root] = avatar
			name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			preview := widget.NewLabel("")
			preview.Truncation = fyne.TextTruncateEllipsis
			badge := widget.NewLabel("")
			unreadDot := widget.NewLabel("●")
			unreadDot.Hide()
			network := widget.NewLabel("")
			return container.NewHBox(avatar.root, container.NewVBox(name, preview), network, unreadDot, badge)
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(rooms.visible) {
				return
			}
			item := rooms.visible[id]
			row := object.(*fyne.Container)
			rowLabels := row.Objects[1].(*fyne.Container)
			name := rowLabels.Objects[0].(*widget.Label)
			if item.Favorite {
				name.SetText("★ " + item.Name)
			} else {
				name.SetText(item.Name)
			}
			name.TextStyle.Bold = item.Unread > 0 || item.Mentions > 0
			name.Refresh()
			preview := item.Preview
			if item.Muted {
				preview = "Muted · " + preview
			}
			rowLabels.Objects[1].(*widget.Label).SetText(preview)
			network := row.Objects[2].(*widget.Label)
			if item.Bridge != "" {
				network.SetText("[" + item.Bridge + "]")
				network.Show()
			} else {
				network.Hide()
			}
			unreadDot := row.Objects[3].(*widget.Label)
			if item.Unread > 0 && item.Mentions == 0 {
				unreadDot.Show()
			} else {
				unreadDot.Hide()
			}
			badge := row.Objects[4].(*widget.Label)
			if item.Mentions > 0 {
				badge.SetText(fmt.Sprintf("%d", item.Mentions))
			} else if item.Unread > 0 {
				badge.SetText(fmt.Sprintf("%d", item.Unread))
			} else {
				badge.SetText("")
			}
			badge.TextStyle.Bold = item.Mentions > 0
			badge.Refresh()
			avatarRoot := row.Objects[0].(*fyne.Container)
			rooms.avatarViews[avatarRoot].set(item.Avatar, item.Name, rooms.avatars)
		},
	)
	rooms.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(rooms.visible) && rooms.onSelect != nil {
			selected := rooms.visible[id]
			if selected.ID != rooms.selected {
				rooms.selected = selected.ID
				rooms.onSelect(selected)
			}
		}
	}
	filters := container.NewHBox(
		widget.NewButton("All", func() { rooms.setFilter(allRooms) }),
	)
	rooms.unanswered = widget.NewButton("Needs reply (0)", func() { rooms.setFilter(unansweredRooms) })
	filters.Add(rooms.unanswered)
	filters.Add(widget.NewButton("DMs", func() { rooms.setFilter(directRooms) }))
	filters.Add(widget.NewButton("Groups", func() { rooms.setFilter(groupRooms) }))
	actions := container.NewHBox(
		widget.NewButton("Mark answered", func() {
			if rooms.selected != "" && rooms.onAnswered != nil {
				rooms.onAnswered(rooms.selected)
			}
		}),
		widget.NewButton("Undo", func() {
			if rooms.onUndo != nil {
				rooms.onUndo()
			}
		}),
	)
	actions.Objects[1].Hide()
	rooms.controls = container.NewVBox(search, filters, actions)
	rooms.undoVisible = false
	rooms.actions = actions
	return rooms
}

func (r *RoomList) SetAvatarCache(cache *avatarCache) {
	r.avatars = cache
	r.list.Refresh()
}

func (r *RoomList) Object() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("Chats", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewBorder(
		container.NewVBox(title, r.controls),
		container.NewHBox(widget.NewButton("Theme", r.theme), widget.NewButton("Settings", r.settings), widget.NewButton("Log out", r.logout)),
		nil, nil, container.NewStack(r.list, r.empty),
	)
}

func (r *RoomList) SetSettingsAction(settingsAction func()) {
	r.onSettings = settingsAction
}

func (r *RoomList) settings() {
	if r.onSettings != nil {
		r.onSettings()
	}
}

func (r *RoomList) SetActions(themeAction, logoutAction func()) {
	r.onTheme = themeAction
	r.onLogout = logoutAction
}

func (r *RoomList) theme() {
	if r.onTheme != nil {
		r.onTheme()
	}
}

func (r *RoomList) logout() {
	if r.onLogout != nil {
		r.onLogout()
	}
}

func (r *RoomList) SetRooms(rooms []matrix.Room) {
	next := append([]matrix.Room(nil), rooms...)
	sort.SliceStable(next, func(i, j int) bool {
		if next[i].Favorite != next[j].Favorite {
			return next[i].Favorite
		}
		if next[i].LastSeen.Equal(next[j].LastSeen) {
			return strings.ToLower(next[i].Name) < strings.ToLower(next[j].Name)
		}
		return next[i].LastSeen.After(next[j].LastSeen)
	})
	if slices.Equal(r.rooms, next) {
		return
	}
	r.rooms = next
	unansweredCount := 0
	for _, room := range next {
		if room.Unanswered {
			unansweredCount++
		}
	}
	r.unanswered.SetText(fmt.Sprintf("Needs reply (%d)", unansweredCount))
	r.refresh()
	for i, room := range r.visible {
		if room.ID == r.selected {
			r.list.Select(widget.ListItemID(i))
			return
		}
	}
	if r.selected != "" && indexRoom(r.rooms, r.selected) < 0 {
		r.selected = ""
		r.list.UnselectAll()
	}
}

func (r *RoomList) setFilter(filter roomFilter) {
	r.filter = filter
	r.refresh()
}

func (r *RoomList) refresh() {
	r.visible = r.visible[:0]
	query := strings.ToLower(r.query)
	for _, room := range r.rooms {
		if r.filter == unansweredRooms && !room.Unanswered {
			continue
		}
		if (r.filter == directRooms && !room.Direct) || (r.filter == groupRooms && room.Direct) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(room.Name), query) &&
			!strings.Contains(strings.ToLower(room.Preview), query) &&
			!strings.Contains(strings.ToLower(room.ID), query) {
			continue
		}
		r.visible = append(r.visible, room)
	}
	r.list.Refresh()
	if len(r.visible) == 0 {
		switch {
		case len(r.rooms) == 0 && r.syncReady:
			r.empty.SetText("No chats yet. Rooms you join will appear here.")
		case len(r.rooms) == 0:
			r.empty.SetText("Syncing your chats from the homeserver. Large accounts may take a little while.")
		default:
			r.empty.SetText("No chats match this search or filter.")
		}
		r.empty.Refresh()
		r.empty.Show()
	} else {
		r.empty.Hide()
	}
}

func indexRoom(rooms []matrix.Room, roomID string) int {
	for i, room := range rooms {
		if room.ID == roomID {
			return i
		}
	}
	return -1
}

func (r *RoomList) SetSyncReady(ready bool) {
	if r.syncReady == ready {
		return
	}
	r.syncReady = ready
	r.refresh()
}

func (r *RoomList) SelectRoom(roomID string) {
	for i, room := range r.visible {
		if room.ID == roomID {
			r.selected = roomID
			r.list.Select(widget.ListItemID(i))
			return
		}
	}
	r.selected = roomID
	r.list.UnselectAll()
}
