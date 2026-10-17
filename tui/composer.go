package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"mychat/matrix"
)

type composerModel struct {
	textarea textarea.Model
	replyTo  *matrix.Message
	editMsg  *matrix.Message
	mode     ComposerMode
}

type ComposerMode int

const (
	ModeNormal ComposerMode = iota
	ModeReply
	ModeEdit
)

func newComposerModel() composerModel {
	ta := textarea.New()
	ta.Placeholder = "type a message..."
	ta.CharLimit = 0
	ta.SetWidth(60)
	ta.SetHeight(2)
	ta.Blur()
	return composerModel{
		textarea: ta,
		mode:     ModeNormal,
	}
}

func (m composerModel) Update(msg tea.Msg) (composerModel, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m composerModel) View() string {
	return m.textarea.View()
}

type ComposeSendMsg struct {
	Content string
	ReplyTo string
	EditID  string
}
