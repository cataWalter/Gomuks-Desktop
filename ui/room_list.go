package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"mychat/matrix"
)

type RoomList struct {
	list     *widget.List
	empty    *widget.Label
	rooms    []matrix.Room
	onSelect func(matrix.Room)
	selected string
}

func NewRoomList(onSelect func(matrix.Room)) *RoomList {
	rooms := &RoomList{
		onSelect: onSelect,
		empty:    widget.NewLabel("Syncing your chats from the homeserver. Large accounts may take a little while."),
	}
	rooms.empty.Wrapping = fyne.TextWrapWord
	rooms.list = widget.NewList(
		func() int { return len(rooms.rooms) },
		func() fyne.CanvasObject {
			return container.NewVBox(widget.NewLabel(""), widget.NewLabel(""))
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if id < 0 || id >= len(rooms.rooms) {
				return
			}
			item := rooms.rooms[id]
			box := object.(*fyne.Container)
			box.Objects[0].(*widget.Label).SetText(item.Name)
			box.Objects[1].(*widget.Label).SetText(item.Preview)
		},
	)
	rooms.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(rooms.rooms) && rooms.onSelect != nil {
			selected := rooms.rooms[id]
			if selected.ID != rooms.selected {
				rooms.selected = selected.ID
				rooms.onSelect(selected)
			}
		}
	}
	return rooms
}

func (r *RoomList) Object() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("Chats", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewBorder(title, nil, nil, nil, container.NewStack(r.list, r.empty))
}

func (r *RoomList) SetRooms(rooms []matrix.Room) {
	r.rooms = append(r.rooms[:0], rooms...)
	r.list.Refresh()
	if len(r.rooms) == 0 {
		r.empty.Show()
	} else {
		r.empty.Hide()
	}
	for i, room := range r.rooms {
		if room.ID == r.selected {
			r.list.Select(widget.ListItemID(i))
			return
		}
	}
	if r.selected != "" {
		r.selected = ""
		r.list.UnselectAll()
	}
}

func (r *RoomList) SetSyncReady(ready bool) {
	if ready {
		r.empty.SetText("No chats yet. Rooms you join will appear here.")
	} else {
		r.empty.SetText("Syncing your chats from the homeserver. Large accounts may take a little while.")
	}
	r.empty.Refresh()
}

func (r *RoomList) SelectRoom(roomID string) {
	for i, room := range r.rooms {
		if room.ID == roomID {
			r.selected = roomID
			r.list.Select(widget.ListItemID(i))
			return
		}
	}
	r.selected = ""
	r.list.UnselectAll()
}
