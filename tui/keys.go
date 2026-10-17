package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Quit             key.Binding
	Help             key.Binding
	FocusRooms       key.Binding
	FocusTimeline    key.Binding
	FocusComposer    key.Binding
	FilterAll        key.Binding
	FilterUnread     key.Binding
	FilterFavorite   key.Binding
	FilterNeeds      key.Binding
	Reply            key.Binding
	Edit             key.Binding
	MarkRead         key.Binding
	NextRoom         key.Binding
	PrevRoom         key.Binding
	OpenMedia        key.Binding
	PlayAudio        key.Binding
	RecordVoice      key.Binding
	SelectMicrophone key.Binding
	ViewImage        key.Binding
	Retry            key.Binding
	Delete           key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Quit, k.Help, k.NextRoom, k.PrevRoom, k.FocusComposer, k.Reply, k.Edit, k.MarkRead,
	}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.Help},
		{k.FocusRooms, k.FocusTimeline, k.FocusComposer},
		{k.FilterAll, k.FilterUnread, k.FilterFavorite, k.FilterNeeds},
		{k.NextRoom, k.PrevRoom, k.Reply, k.Edit, k.MarkRead, k.OpenMedia, k.PlayAudio, k.RecordVoice, k.SelectMicrophone, k.ViewImage, k.Retry, k.Delete},
	}
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q/ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	FocusRooms: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "focus rooms"),
	),
	FocusTimeline: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "focus timeline"),
	),
	FocusComposer: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "focus composer"),
	),
	FilterAll: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "all rooms"),
	),
	FilterUnread: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "unread rooms"),
	),
	FilterFavorite: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "favorite rooms"),
	),
	FilterNeeds: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "rooms needing reply"),
	),
	Reply: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "reply"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit"),
	),
	MarkRead: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "mark read"),
	),
	NextRoom: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/down", "next room"),
	),
	PrevRoom: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/up", "prev room"),
	),
	OpenMedia: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open attachment"),
	),
	PlayAudio: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "play audio message"),
	),
	RecordVoice: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "record voice note"),
	),
	SelectMicrophone: key.NewBinding(
		key.WithKeys("ctrl+m"),
		key.WithHelp("ctrl+m", "select microphone"),
	),
	ViewImage: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "view image natively"),
	),
	Retry: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "retry failed message"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete own message (twice)"),
	),
}

type helpModel struct {
	help help.Model
}

func newHelpModel() helpModel {
	h := help.New()
	h.ShowAll = true
	return helpModel{help: h}
}
