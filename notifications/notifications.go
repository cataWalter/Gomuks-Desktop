package notifications

import "context"

type Notifier interface {
	Supported() bool
	Permission() string
	RequestPermission() (string, error)
	Send(title, body, roomID string, sound bool) error
}

type Lifecycle interface {
	Start(context.Context) error
	Close() error
}

type ClickHandler interface {
	SetClickHandler(func(roomID string))
}
