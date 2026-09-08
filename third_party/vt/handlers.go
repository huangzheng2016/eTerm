package vt

import (
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type DcsHandler func(params ansi.Params, data []byte) bool

type CsiHandler func(params ansi.Params) bool

type OscHandler func(data []byte) bool

type ApcHandler func(data []byte) bool

type SosHandler func(data []byte) bool

type PmHandler func(data []byte) bool

type EscHandler func() bool

type CcHandler func() bool

type handlers struct {
	ccHandlers  map[byte][]CcHandler
	dcsHandlers map[int][]DcsHandler
	csiHandlers map[int][]CsiHandler
	oscHandlers map[int][]OscHandler
	escHandler  map[int][]EscHandler
	apcHandlers []ApcHandler
	sosHandlers []SosHandler
	pmHandlers  []PmHandler
}

func (h *handlers) RegisterDcsHandler(cmd int, handler DcsHandler) {
	if h.dcsHandlers == nil {
		h.dcsHandlers = make(map[int][]DcsHandler)
	}
	h.dcsHandlers[cmd] = append(h.dcsHandlers[cmd], handler)
}

func (h *handlers) RegisterCsiHandler(cmd int, handler CsiHandler) {
	if h.csiHandlers == nil {
		h.csiHandlers = make(map[int][]CsiHandler)
	}
	h.csiHandlers[cmd] = append(h.csiHandlers[cmd], handler)
}

func (h *handlers) RegisterOscHandler(cmd int, handler OscHandler) {
	if h.oscHandlers == nil {
		h.oscHandlers = make(map[int][]OscHandler)
	}
	h.oscHandlers[cmd] = append(h.oscHandlers[cmd], handler)
}

func (h *handlers) RegisterApcHandler(handler ApcHandler) {
	h.apcHandlers = append(h.apcHandlers, handler)
}

func (h *handlers) RegisterSosHandler(handler SosHandler) {
	h.sosHandlers = append(h.sosHandlers, handler)
}

func (h *handlers) RegisterPmHandler(handler PmHandler) {
	h.pmHandlers = append(h.pmHandlers, handler)
}

func (h *handlers) RegisterEscHandler(cmd int, handler EscHandler) {
	if h.escHandler == nil {
		h.escHandler = make(map[int][]EscHandler)
	}
	h.escHandler[cmd] = append(h.escHandler[cmd], handler)
}

func (h *handlers) registerCcHandler(r byte, handler CcHandler) {
	if h.ccHandlers == nil {
		h.ccHandlers = make(map[byte][]CcHandler)
	}
	h.ccHandlers[r] = append(h.ccHandlers[r], handler)
}

func (h *handlers) handleCc(r byte) bool {
	for i := len(h.ccHandlers[r]) - 1; i >= 0; i-- {
		if h.ccHandlers[r][i]() {
			return true
		}
	}
	return false
}

func (h *handlers) handleDcs(cmd ansi.Cmd, params ansi.Params, data []byte) bool {
	if handlers, ok := h.dcsHandlers[int(cmd)]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i](params, data) {
				return true
			}
		}
	}
	return false
}

func (h *handlers) handleCsi(cmd ansi.Cmd, params ansi.Params) bool {
	if handlers, ok := h.csiHandlers[int(cmd)]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i](params) {
				return true
			}
		}
	}
	return false
}

func (h *handlers) handleOsc(cmd int, data []byte) bool {
	if handlers, ok := h.oscHandlers[cmd]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i](data) {
				return true
			}
		}
	}
	return false
}

func (h *handlers) handleApc(data []byte) bool {
	for i := len(h.apcHandlers) - 1; i >= 0; i-- {
		if h.apcHandlers[i](data) {
			return true
		}
	}
	return false
}

func (h *handlers) handleSos(data []byte) bool {
	for i := len(h.sosHandlers) - 1; i >= 0; i-- {
		if h.sosHandlers[i](data) {
			return true
		}
	}
	return false
}

func (h *handlers) handlePm(data []byte) bool {
	for i := len(h.pmHandlers) - 1; i >= 0; i-- {
		if h.pmHandlers[i](data) {
			return true
		}
	}
	return false
}

func (h *handlers) handleEsc(cmd int) bool {
	if handlers, ok := h.escHandler[cmd]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i]() {
				return true
			}
		}
	}
	return false
}

func (e *Emulator) registerDefaultHandlers() {
	e.registerDefaultCcHandlers()
	e.registerDefaultCsiHandlers()
	e.registerDefaultEscHandlers()
	e.registerDefaultOscHandlers()
}

func (e *Emulator) registerDefaultCcHandlers() {
	for i := byte(ansi.NUL); i <= ansi.US; i++ {
		switch i {
		case ansi.NUL:
			e.registerCcHandler(i, func() bool {
				return true
			})
		case ansi.BEL:
			e.registerCcHandler(i, func() bool {
				if e.cb.Bell != nil {
					e.cb.Bell()
				}
				return true
			})
		case ansi.BS:
			e.registerCcHandler(i, func() bool {
				e.backspace()
				return true
			})
		case ansi.HT:
			e.registerCcHandler(i, func() bool {
				e.nextTab(1)
				return true
			})
		case ansi.LF, ansi.VT, ansi.FF:
			e.registerCcHandler(i, func() bool {
				e.linefeed()
				return true
			})
		case ansi.CR:
			e.registerCcHandler(i, func() bool {
				e.carriageReturn()
				return true
			})
		}
	}

	for i := byte(ansi.PAD); i <= byte(ansi.APC); i++ {
		switch i {
		case ansi.HTS:
			e.registerCcHandler(i, func() bool {
				e.horizontalTabSet()
				return true
			})
		case ansi.RI:
			e.registerCcHandler(i, func() bool {
				e.reverseIndex()
				return true
			})
		case ansi.SO:
			e.registerCcHandler(i, func() bool {
				e.gl = 1
				return true
			})
		case ansi.SI:
			e.registerCcHandler(i, func() bool {
				e.gl = 0
				return true
			})
		case ansi.IND:
			e.registerCcHandler(i, func() bool {
				e.index()
				return true
			})
		case ansi.SS2:
			e.registerCcHandler(i, func() bool {
				e.gsingle = 2
				return true
			})
		case ansi.SS3:
			e.registerCcHandler(i, func() bool {
				e.gsingle = 3
				return true
			})
		}
	}
}

func (e *Emulator) registerDefaultOscHandlers() {
	for _, cmd := range []int{
		0,
		1,
		2,
	} {
		e.RegisterOscHandler(cmd, func(data []byte) bool {
			e.handleTitle(cmd, data)
			return true
		})
	}

	e.RegisterOscHandler(7, func(data []byte) bool {
		e.handleWorkingDirectory(7, data)
		return true
	})

	e.RegisterOscHandler(8, func(data []byte) bool {
		e.handleHyperlink(8, data)
		return true
	})

	e.RegisterOscHandler(9, func(data []byte) bool {
		e.handleNotification(9, data)
		return true
	})

	e.RegisterOscHandler(133, func(data []byte) bool {
		e.handleCommandSequence(133, data)
		return true
	})

	e.RegisterOscHandler(777, func(data []byte) bool {
		e.handleUrxvtNotify(777, data)
		return true
	})

	for _, cmd := range []int{
		10,
		11,
		12,
		110,
		111,
		112,
	} {
		e.RegisterOscHandler(cmd, func(data []byte) bool {
			e.handleDefaultColor(cmd, data)
			return true
		})
	}
}

func (e *Emulator) registerDefaultEscHandlers() {
	e.RegisterEscHandler('=', func() bool {
		e.setMode(ansi.ModeNumericKeypad, ansi.ModeSet)
		return true
	})

	e.RegisterEscHandler('>', func() bool {
		e.setMode(ansi.ModeNumericKeypad, ansi.ModeReset)
		return true
	})

	e.RegisterEscHandler('7', func() bool {
		e.scr.SaveCursor()
		return true
	})

	e.RegisterEscHandler('8', func() bool {
		e.scr.RestoreCursor()
		return true
	})

	for _, cmd := range []int{
		ansi.Command(0, '(', 'A'),
		ansi.Command(0, ')', 'A'),
		ansi.Command(0, '*', 'A'),
		ansi.Command(0, '+', 'A'),
		ansi.Command(0, '(', 'B'),
		ansi.Command(0, ')', 'B'),
		ansi.Command(0, '*', 'B'),
		ansi.Command(0, '+', 'B'),
		ansi.Command(0, '(', '0'),
		ansi.Command(0, ')', '0'),
		ansi.Command(0, '*', '0'),
		ansi.Command(0, '+', '0'),
	} {
		e.RegisterEscHandler(cmd, func() bool {
			c := ansi.Cmd(cmd)
			set := c.Intermediate() - '('
			switch c.Final() {
			case 'A':
				e.charsets[set] = UK
			case 'B':
				e.charsets[set] = nil
			case '0':
				e.charsets[set] = SpecialDrawing
			default:
				return false
			}
			return true
		})
	}

	e.RegisterEscHandler('D', func() bool {
		e.index()
		return true
	})

	e.RegisterEscHandler('H', func() bool {
		e.horizontalTabSet()
		return true
	})

	e.RegisterEscHandler('M', func() bool {
		e.reverseIndex()
		return true
	})

	e.RegisterEscHandler('c', func() bool {
		e.fullReset()
		return true
	})

	e.RegisterEscHandler('n', func() bool {
		e.gl = 2
		return true
	})

	e.RegisterEscHandler('o', func() bool {
		e.gl = 3
		return true
	})

	e.RegisterEscHandler('|', func() bool {
		e.gr = 3
		return true
	})

	e.RegisterEscHandler('}', func() bool {
		e.gr = 2
		return true
	})

	e.RegisterEscHandler('~', func() bool {
		e.gr = 1
		return true
	})
}

func (e *Emulator) registerDefaultCsiHandlers() {
	e.RegisterCsiHandler('@', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.scr.InsertCell(n)
		return true
	})

	e.RegisterCsiHandler('A', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, -n)
		return true
	})

	e.RegisterCsiHandler('B', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, n)
		return true
	})

	e.RegisterCsiHandler('C', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.moveCursor(n, 0)
		return true
	})

	e.RegisterCsiHandler('D', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.moveCursor(-n, 0)
		return true
	})

	e.RegisterCsiHandler('E', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, n)
		e.carriageReturn()
		return true
	})

	e.RegisterCsiHandler('F', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, -n)
		e.carriageReturn()
		return true
	})

	e.RegisterCsiHandler('G', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		_, y := e.scr.CursorPosition()
		e.setCursor(n-1, y)
		return true
	})

	e.RegisterCsiHandler('H', func(params ansi.Params) bool {
		width, height := e.Width(), e.Height()
		row, _, _ := params.Param(0, 1)
		col, _, _ := params.Param(1, 1)
		if row < 1 {
			row = 1
		}
		if col < 1 {
			col = 1
		}
		y := min(height-1, row-1)
		x := min(width-1, col-1)
		e.setCursorPosition(x, y)
		return true
	})

	e.RegisterCsiHandler('I', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.nextTab(n)
		return true
	})

	e.RegisterCsiHandler('J', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		width, height := e.Width(), e.Height()
		x, y := e.scr.CursorPosition()
		switch n {
		case 0:
			rect1 := uv.Rect(x, y, width, 1)
			rect2 := uv.Rect(0, y+1, width, height-y-1)
			e.scr.FillArea(e.scr.blankCell(), rect1)
			e.scr.FillArea(e.scr.blankCell(), rect2)
		case 1:
			rect := uv.Rect(0, 0, width, y+1)
			e.scr.FillArea(e.scr.blankCell(), rect)
		case 2:
			e.scr.ClearWithScrollback()
		case 3:
			e.scr.Clear()
			if sb := e.scr.Scrollback(); sb != nil {
				sb.Clear()
			}
		default:
			return false
		}
		return true
	})

	e.RegisterCsiHandler('K', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		x, y := e.scr.CursorPosition()
		w := e.scr.Width()

		switch n {
		case 0:
			e.eraseCharacter(w - x)
		case 1:
			rect := uv.Rect(0, y, x+1, 1)
			e.scr.FillArea(e.scr.blankCell(), rect)
		case 2:
			rect := uv.Rect(0, y, w, 1)
			e.scr.FillArea(e.scr.blankCell(), rect)
		default:
			return false
		}
		return true
	})

	e.RegisterCsiHandler('L', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		if e.scr.InsertLine(n) {
			e.scr.setCursorX(0, true)
		}
		return true
	})

	e.RegisterCsiHandler('M', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		if e.scr.DeleteLine(n) {
			e.scr.setCursorX(0, true)
		}
		return true
	})

	e.RegisterCsiHandler('P', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.scr.DeleteCell(n)
		return true
	})

	e.RegisterCsiHandler('S', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.scr.ScrollUp(n)
		return true
	})

	e.RegisterCsiHandler('T', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.scr.ScrollDown(n)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'W'), func(params ansi.Params) bool {
		if len(params) == 1 && params[0] == 5 {
			e.resetTabStops()
			return true
		}
		return false
	})

	e.RegisterCsiHandler('X', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.eraseCharacter(n)
		return true
	})

	e.RegisterCsiHandler('Z', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.prevTab(n)
		return true
	})

	e.RegisterCsiHandler('`', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		width := e.Width()
		_, y := e.scr.CursorPosition()
		e.setCursorPosition(min(width-1, n-1), y)
		return true
	})

	e.RegisterCsiHandler('a', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		width := e.Width()
		x, y := e.scr.CursorPosition()
		e.setCursorPosition(min(width-1, x+n), y)
		return true
	})

	e.RegisterCsiHandler('b', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		e.repeatPreviousCharacter(n)
		return true
	})

	e.RegisterCsiHandler('c', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		if n != 0 {
			return false
		}

		_, _ = io.WriteString(e.pw, ansi.PrimaryDeviceAttributes(
			62,
			1,
			6,
			22,
		))
		return true
	})

	e.RegisterCsiHandler(ansi.Command('>', 0, 'c'), func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		if n != 0 {
			return false
		}

		_, _ = io.WriteString(e.pw, ansi.SecondaryDeviceAttributes(
			1,
			10,
			0,
		))
		return true
	})

	e.RegisterCsiHandler('d', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		height := e.Height()
		x, _ := e.scr.CursorPosition()
		e.setCursorPosition(x, min(height-1, n-1))
		return true
	})

	e.RegisterCsiHandler('e', func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		height := e.Height()
		x, y := e.scr.CursorPosition()
		e.setCursorPosition(x, min(height-1, y+n))
		return true
	})

	e.RegisterCsiHandler('f', func(params ansi.Params) bool {
		width, height := e.Width(), e.Height()
		row, _, _ := params.Param(0, 1)
		col, _, _ := params.Param(1, 1)
		y := min(height-1, row-1)
		x := min(width-1, col-1)
		e.setCursor(x, y)
		return true
	})

	e.RegisterCsiHandler('g', func(params ansi.Params) bool {
		value, _, _ := params.Param(0, 0)
		switch value {
		case 0:
			x, _ := e.scr.CursorPosition()
			e.tabstops.Reset(x)
		case 3:
			e.tabstops.Clear()
		default:
			return false
		}

		return true
	})

	e.RegisterCsiHandler('h', func(params ansi.Params) bool {
		e.handleMode(params, true, true)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'h'), func(params ansi.Params) bool {
		e.handleMode(params, true, false)
		return true
	})

	e.RegisterCsiHandler('l', func(params ansi.Params) bool {
		e.handleMode(params, false, true)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'l'), func(params ansi.Params) bool {
		e.handleMode(params, false, false)
		return true
	})

	e.RegisterCsiHandler('m', func(params ansi.Params) bool {
		e.handleSgr(params)
		return true
	})

	e.RegisterCsiHandler('n', func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 1)
		if !ok || n == 0 {
			return false
		}

		switch n {
		case 5:
			_, _ = io.WriteString(e.pw, ansi.DeviceStatusReport(ansi.DECStatusReport(0)))
		case 6:
			x, y := e.scr.CursorPosition()
			_, _ = io.WriteString(e.pw, ansi.CursorPositionReport(y+1, x+1))
		default:
			return false
		}

		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'n'), func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 1)
		if !ok || n == 0 {
			return false
		}

		switch n {
		case 6:
			x, y := e.scr.CursorPosition()
			_, _ = io.WriteString(e.pw, ansi.ExtendedCursorPositionReport(y+1, x+1, 0)) // We don't support page numbers //nolint:errcheck
		default:
			return false
		}

		return true
	})

	e.RegisterCsiHandler(ansi.Command(0, '$', 'p'), func(params ansi.Params) bool {
		e.handleRequestMode(params, true)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', '$', 'p'), func(params ansi.Params) bool {
		e.handleRequestMode(params, false)
		return true
	})

	e.RegisterCsiHandler(ansi.Command(0, ' ', 'q'), func(params ansi.Params) bool {
		n := 1
		if param, _, ok := params.Param(0, 0); ok && param > n {
			n = param
		}
		blink := n == 0 || n%2 == 1
		style := n / 2
		if !blink {
			style--
		}
		e.scr.setCursorStyle(CursorStyle(style), blink)
		return true
	})

	e.RegisterCsiHandler('r', func(params ansi.Params) bool {
		top, _, _ := params.Param(0, 1)
		if top < 1 {
			top = 1
		}

		height := e.Height()
		bottom, _ := e.parser.Param(1, height)
		if bottom < 1 {
			bottom = height
		}

		if top >= bottom {
			return false
		}

		e.scr.setVerticalMargins(top-1, bottom)

		e.setCursorPosition(0, 0)
		return true
	})

	e.RegisterCsiHandler('s', func(params ansi.Params) bool {

		if e.isModeSet(ansi.ModeLeftRightMargin) {
			left, _, _ := params.Param(0, 1)
			if left < 1 {
				left = 1
			}

			width := e.Width()
			right, _, _ := params.Param(1, width)
			if right < 1 {
				right = width
			}

			if left >= right {
				return false
			}

			e.scr.setHorizontalMargins(left-1, right)

			e.setCursorPosition(0, 0)
		} else {
			e.scr.SaveCursor()
		}

		return true
	})
}
