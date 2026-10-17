package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type loginModel struct {
	homeserver textinput.Model
	username   textinput.Model
	password   textinput.Model
	focus      int
	loading    bool
	err        string
}

func newLoginModel() loginModel {
	homeserver := textinput.New()
	homeserver.Placeholder = "https://matrix.org"
	homeserver.Prompt = "Homeserver  "
	homeserver.Width = 48
	homeserver.Focus()

	username := textinput.New()
	username.Placeholder = "@you:matrix.org"
	username.Prompt = "Username    "
	username.Width = 48

	password := textinput.New()
	password.Placeholder = "Password"
	password.Prompt = "Password    "
	password.Width = 48
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '*'

	return loginModel{homeserver: homeserver, username: username, password: password}
}

func (m loginModel) Update(msg tea.Msg) (loginModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "tab", "down", "enter":
			if keyMsg.String() == "enter" && m.focus == 2 {
				homeserver := strings.TrimSpace(m.homeserver.Value())
				username := strings.TrimSpace(m.username.Value())
				if homeserver == "" || username == "" || strings.TrimSpace(m.password.Value()) == "" {
					m.err = "Enter your homeserver, username, and password."
					return m, nil
				}
				m.err = ""
				return m, func() tea.Msg {
					return LoginSubmitMsg{
						Homeserver: homeserver,
						Username:   username,
						Password:   m.password.Value(),
					}
				}
			}
			m.setFocus((m.focus + 1) % 3)
			return m, nil
		case "shift+tab", "up":
			m.setFocus((m.focus + 2) % 3)
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case 0:
		m.homeserver, cmd = m.homeserver.Update(msg)
	case 1:
		m.username, cmd = m.username.Update(msg)
	case 2:
		m.password, cmd = m.password.Update(msg)
	}
	return m, cmd
}

func (m loginModel) View() string {
	status := "Log in to your Matrix account"
	if m.loading {
		status = "Connecting to Matrix..."
	}
	if m.err != "" {
		status = lipgloss.NewStyle().Foreground(mention).Render(m.err)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(accent).Bold(true).Render("PINGER / MATRIX"),
		status,
		"",
		m.homeserver.View(),
		m.username.View(),
		m.password.View(),
		"",
		footerStyle.Render("tab / shift+tab move | enter on password logs in | ctrl+c quits"),
	)
}

type LoginSubmitMsg struct {
	Homeserver string
	Username   string
	Password   string
}

type LoginResultMsg struct {
	Err error
}

func (m *loginModel) setFocus(index int) {
	m.homeserver.Blur()
	m.username.Blur()
	m.password.Blur()
	m.focus = index
	switch m.focus {
	case 0:
		m.homeserver.Focus()
	case 1:
		m.username.Focus()
	case 2:
		m.password.Focus()
	}
}
