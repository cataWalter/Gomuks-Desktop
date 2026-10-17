package tui

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"mychat/matrix"
)

func Run(client *matrix.Client) error {
	if client == nil {
		return fmt.Errorf("Matrix client is required")
	}
	model := NewModel(client)
	model.nativeImages = os.Getenv("GOMUKS_TUI_IMAGES") == "1"
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		client.Close()
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update, ok := <-client.Events():
				if !ok {
					return
				}
				p.Send(MatrixUpdateMsg{Update: update})
			}
		}
	}()
	if client.IsLoggedIn() {
		go func() {
			if err := client.StartWithRetry(ctx); err != nil && ctx.Err() == nil {
				p.Send(MatrixUpdateMsg{Update: matrix.Update{Error: err.Error(), ConnectionState: matrix.StateOffline}})
			}
		}()
	}

	_, err := p.Run()
	return err
}
