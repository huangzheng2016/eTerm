package vt

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type Callbacks struct {
	Bell func()

	Title func(string)

	IconName func(string)

	AltScreen func(bool)

	CursorPosition func(old, new uv.Position) //nolint:predeclared,revive

	CursorVisibility func(visible bool)

	CursorStyle func(style CursorStyle, blink bool)

	CursorColor func(color color.Color)

	BackgroundColor func(color color.Color)

	ForegroundColor func(color color.Color)

	WorkingDirectory func(string)

	Notification func(string)

	PromptStart func()

	InputStart func()

	CommandStart func()

	CommandEnd func(exitCode int)

	EnableMode func(mode ansi.Mode)

	DisableMode func(mode ansi.Mode)
}
