package vt

import (
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type MouseButton = uv.MouseButton

const (
	MouseNone       = uv.MouseNone
	MouseLeft       = uv.MouseLeft
	MouseMiddle     = uv.MouseMiddle
	MouseRight      = uv.MouseRight
	MouseWheelUp    = uv.MouseWheelUp
	MouseWheelDown  = uv.MouseWheelDown
	MouseWheelLeft  = uv.MouseWheelLeft
	MouseWheelRight = uv.MouseWheelRight
	MouseBackward   = uv.MouseBackward
	MouseForward    = uv.MouseForward
	MouseButton10   = uv.MouseButton10
	MouseButton11   = uv.MouseButton11
)

type Mouse = uv.MouseEvent

type MouseClick = uv.MouseClickEvent

type MouseRelease = uv.MouseReleaseEvent

type MouseWheel = uv.MouseWheelEvent

type MouseMotion = uv.MouseMotionEvent

func (e *Emulator) SendMouse(m Mouse) {
	var (
		enc  ansi.Mode
		mode ansi.Mode
	)

	for _, m := range []ansi.DECMode{
		ansi.ModeMouseX10,
		ansi.ModeMouseNormal,
		ansi.ModeMouseHighlight,
		ansi.ModeMouseButtonEvent,
		ansi.ModeMouseAnyEvent,
	} {
		if e.isModeSet(m) {
			mode = m
		}
	}

	if mode == nil {
		return
	}

	for _, mm := range []ansi.DECMode{
		ansi.ModeMouseExtSgr,
	} {
		if e.isModeSet(mm) {
			enc = mm
		}
	}

	mouse := m.Mouse()
	_, isMotion := m.(MouseMotion)
	_, isRelease := m.(MouseRelease)
	b := ansi.EncodeMouseButton(mouse.Button, isMotion,
		mouse.Mod.Contains(ModShift),
		mouse.Mod.Contains(ModAlt),
		mouse.Mod.Contains(ModCtrl))

	switch enc {
	case nil:
		_, _ = io.WriteString(e.pw, ansi.MouseX10(b, mouse.X, mouse.Y))
	case ansi.ModeMouseExtSgr:
		_, _ = io.WriteString(e.pw, ansi.MouseSgr(b, mouse.X, mouse.Y, isRelease))
	}
}
