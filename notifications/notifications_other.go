//go:build !darwin || !cgo

package notifications

type nativeNotifier struct{}

func New() Notifier {
	return nativeNotifier{}
}

func (nativeNotifier) Supported() bool {
	return false
}

func (nativeNotifier) Permission() string {
	return "unsupported"
}

func (nativeNotifier) RequestPermission() (string, error) {
	return "unsupported", nil
}

func (nativeNotifier) Send(string, string, string, bool) error {
	return nil
}
