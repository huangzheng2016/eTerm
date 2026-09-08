package vt

import (
	"bytes"
	"image/color"
	"io"
	"strconv"

	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) handleOsc(cmd int, data []byte) {
	e.flushGrapheme()
	if !e.handlers.handleOsc(cmd, data) {
		e.logf("unhandled sequence: OSC %q", data)
	}
}

func (e *Emulator) handleTitle(cmd int, data []byte) {
	parts := bytes.Split(data, []byte{';'})
	if len(parts) != 2 {
		return
	}
	switch cmd {
	case 0:
		name := string(parts[1])
		e.iconName, e.title = name, name
		if e.cb.Title != nil {
			e.cb.Title(name)
		}
		if e.cb.IconName != nil {
			e.cb.IconName(name)
		}
	case 1:
		name := string(parts[1])
		e.iconName = name
		if e.cb.IconName != nil {
			e.cb.IconName(name)
		}
	case 2:
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
		return
	}

	parts := bytes.Split(data, []byte{';'})
	if len(parts) == 0 {
		return
	}

	cb := func(c color.Color) {
		switch cmd {
		case 10, 110:
			e.SetForegroundColor(c)
		case 11, 111:
			e.SetBackgroundColor(c)
		case 12, 112:
			e.SetCursorColor(c)
		}
	}

	switch len(parts) {
	case 1:
		cb(nil)
	case 2:
		arg := string(parts[1])
		if arg == "?" {
			var xrgb ansi.XRGBColor
			switch cmd {
			case 10:
				xrgb.Color = e.ForegroundColor()
				if xrgb.Color != nil {
					io.WriteString(e.pw, ansi.SetForegroundColor(xrgb.String())) //nolint:errcheck,gosec
				}
			case 11:
				xrgb.Color = e.BackgroundColor()
				if xrgb.Color != nil {
					io.WriteString(e.pw, ansi.SetBackgroundColor(xrgb.String())) //nolint:errcheck,gosec
				}
			case 12:
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
		return
	}

	parts := bytes.Split(data, []byte{';'})
	if len(parts) != 2 {
		return
	}

	path := string(parts[1])
	e.cwd = path

	if e.cb.WorkingDirectory != nil {
		e.cb.WorkingDirectory(path)
	}
}

func (e *Emulator) handleNotification(cmd int, data []byte) {
	if cmd != 9 {
		return
	}

	parts := bytes.SplitN(data, []byte{';'}, 2)
	if len(parts) != 2 || len(parts[1]) == 0 {
		return
	}

	if e.cb.Notification != nil {
		e.cb.Notification(string(parts[1]))
	}
}

func (e *Emulator) handleUrxvtNotify(cmd int, data []byte) {
	if cmd != 777 {
		return
	}

	parts := bytes.SplitN(data, []byte{';'}, 4)
	if len(parts) != 4 || string(parts[1]) != "notify" {
		return
	}

	text := string(parts[2])
	if body := string(parts[3]); body != "" {
		if text != "" {
			text += ": "
		}
		text += body
	}
	if text == "" {
		return
	}

	if e.cb.Notification != nil {
		e.cb.Notification(text)
	}
}

func (e *Emulator) handleCommandSequence(cmd int, data []byte) {
	if cmd != 133 {
		return
	}

	parts := bytes.Split(data, []byte{';'})
	if len(parts) < 2 || len(parts[1]) == 0 {
		return
	}

	switch parts[1][0] {
	case 'A':
		if e.cb.PromptStart != nil {
			e.cb.PromptStart()
		}
	case 'B':
		if e.cb.InputStart != nil {
			e.cb.InputStart()
		}
	case 'C':
		if e.cb.CommandStart != nil {
			e.cb.CommandStart()
		}
	case 'D':
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
		return
	}

	e.scr.cur.Link.Params = string(parts[1])
	e.scr.cur.Link.URL = string(parts[2])
}
