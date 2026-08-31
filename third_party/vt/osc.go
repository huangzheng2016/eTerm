// Package vt provides a virtual terminal implementation.
// SKIP: Fix typecheck errors - function signature mismatches and undefined types
package vt

import (
	"bytes"
	"image/color"
	"io"
	"strconv"

	"github.com/charmbracelet/x/ansi"
)

// handleOsc handles an OSC escape sequence.
func (e *Emulator) handleOsc(cmd int, data []byte) {
	e.flushGrapheme() // Flush any pending grapheme before handling OSC sequences.
	if !e.handlers.handleOsc(cmd, data) {
		e.logf("unhandled sequence: OSC %q", data)
	}
}

func (e *Emulator) handleTitle(cmd int, data []byte) {
	parts := bytes.Split(data, []byte{';'})
	if len(parts) != 2 {
		// Invalid, ignore
		return
	}
	switch cmd {
	case 0: // Set window title and icon name
		name := string(parts[1])
		e.iconName, e.title = name, name
		if e.cb.Title != nil {
			e.cb.Title(name)
		}
		if e.cb.IconName != nil {
			e.cb.IconName(name)
		}
	case 1: // Set icon name
		name := string(parts[1])
		e.iconName = name
		if e.cb.IconName != nil {
			e.cb.IconName(name)
		}
	case 2: // Set window title
		name := string(parts[1])
		e.title = name
		if e.cb.Title != nil {
			e.cb.Title(name)
		}
	}
}

func (e *Emulator) handleDefaultColor(cmd int, data []byte) {
	if cmd != 10 && cmd != 11 && cmd != 12 &&
		cmd != 110 && cmd != 111 && cmd != 112 {
		// Invalid, ignore
		return
	}

	parts := bytes.Split(data, []byte{';'})
	if len(parts) == 0 {
		// Invalid, ignore
		return
	}

	cb := func(c color.Color) {
		switch cmd {
		case 10, 110: // Foreground color
			e.SetForegroundColor(c)
		case 11, 111: // Background color
			e.SetBackgroundColor(c)
		case 12, 112: // Cursor color
			e.SetCursorColor(c)
		}
	}

	switch len(parts) {
	case 1: // Reset color
		cb(nil)
	case 2: // Set/Query color
		arg := string(parts[1])
		if arg == "?" {
			var xrgb ansi.XRGBColor
			switch cmd {
			case 10: // Query foreground color
				xrgb.Color = e.ForegroundColor()
				if xrgb.Color != nil {
					io.WriteString(e.pw, ansi.SetForegroundColor(xrgb.String())) //nolint:errcheck,gosec
				}
			case 11: // Query background color
				xrgb.Color = e.BackgroundColor()
				if xrgb.Color != nil {
					io.WriteString(e.pw, ansi.SetBackgroundColor(xrgb.String())) //nolint:errcheck,gosec
				}
			case 12: // Query cursor color
				xrgb.Color = e.CursorColor()
				if xrgb.Color != nil {
					io.WriteString(e.pw, ansi.SetCursorColor(xrgb.String())) //nolint:errcheck,gosec
				}
			}
		} else if c := ansi.XParseColor(arg); c != nil {
			cb(c)
		}
	}
}

func (e *Emulator) handleWorkingDirectory(cmd int, data []byte) {
	if cmd != 7 {
		// Invalid, ignore
		return
	}

	// The data is the working directory path.
	parts := bytes.Split(data, []byte{';'})
	if len(parts) != 2 {
		// Invalid, ignore
		return
	}

	path := string(parts[1])
	e.cwd = path

	if e.cb.WorkingDirectory != nil {
		e.cb.WorkingDirectory(path)
	}
}

// handleNotification handles OSC 9 iTerm2-style desktop notifications.
func (e *Emulator) handleNotification(cmd int, data []byte) {
	if cmd != 9 {
		// Invalid, ignore
		return
	}

	parts := bytes.SplitN(data, []byte{';'}, 2)
	if len(parts) != 2 || len(parts[1]) == 0 {
		// Invalid, ignore
		return
	}

	if e.cb.Notification != nil {
		e.cb.Notification(string(parts[1]))
	}
}

// handleCommandSequence handles OSC 133 shell command lifecycle markers.
// A = prompt start, B = input start, C = command execution start,
// D;<exitcode> = command finished.
func (e *Emulator) handleCommandSequence(cmd int, data []byte) {
	if cmd != 133 {
		// Invalid, ignore
		return
	}

	parts := bytes.Split(data, []byte{';'})
	if len(parts) < 2 || len(parts[1]) == 0 {
		// Invalid, ignore
		return
	}

	switch parts[1][0] {
	case 'C': // Command execution starts
		if e.cb.CommandStart != nil {
			e.cb.CommandStart()
		}
	case 'D': // Command finished
		exitCode := -1
		if len(parts) >= 3 {
			if n, err := strconv.Atoi(string(parts[2])); err == nil {
				exitCode = n
			}
		}
		if e.cb.CommandEnd != nil {
			e.cb.CommandEnd(exitCode)
		}
	}
}

func (e *Emulator) handleHyperlink(cmd int, data []byte) {
	parts := bytes.Split(data, []byte{';'})
	if len(parts) != 3 || cmd != 8 {
		// Invalid, ignore
		return
	}

	e.scr.cur.Link.URL = string(parts[1])
	e.scr.cur.Link.Params = string(parts[2])
}
