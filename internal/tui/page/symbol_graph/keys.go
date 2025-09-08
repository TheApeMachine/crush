package symbolgraph

import (
	"github.com/charmbracelet/bubbles/v2/key"
)

// KeyMap defines key bindings for the symbol graph page
type KeyMap struct {
	Quit        key.Binding
	ToggleFocus key.Binding
	Search      key.Binding
	Select      key.Binding
	Refresh     key.Binding
	ToggleEdges key.Binding
	ToggleGraph key.Binding
	ZoomIn      key.Binding
	ZoomOut     key.Binding
	PanUp       key.Binding
	PanDown     key.Binding
	PanLeft     key.Binding
	PanRight    key.Binding
}

// DefaultKeyMap returns the default key bindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "q"),
			key.WithHelp("ctrl+c/q", "back to chat"),
		),
		ToggleFocus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "toggle focus"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "reload files"),
		),
		ToggleEdges: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "toggle edges"),
		),
		ToggleGraph: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "toggle graph mode"),
		),
		ZoomIn: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+", "zoom in"),
		),
		ZoomOut: key.NewBinding(
			key.WithKeys("-", "_"),
			key.WithHelp("-", "zoom out"),
		),
		PanUp: key.NewBinding(
			key.WithKeys("K", "k", "up"),
			key.WithHelp("K/↑", "prev item"),
		),
		PanDown: key.NewBinding(
			key.WithKeys("J", "j", "down"),
			key.WithHelp("J/↓", "next item"),
		),
		PanLeft: key.NewBinding(
			key.WithKeys("H", "h", "left"),
			key.WithHelp("H/←", "move left"),
		),
		PanRight: key.NewBinding(
			key.WithKeys("L", "l", "right"),
			key.WithHelp("L/→", "move right"),
		),
	}
}
