package vt

import (
	"image/color"
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

type Logger interface {
	Printf(format string, v ...any)
}

type Emulator struct {
	handlers

	colors [256]color.Color

	scrs [2]Screen
	scr  *Screen

	charsets [4]CharSet

	logger Logger

	defaultFg, defaultBg, defaultCur color.Color
	fgColor, bgColor, curColor       color.Color

	modes ansi.Modes

	lastChar rune
	grapheme []rune

	parser    *ansi.Parser
	lastState parser.State

	cb Callbacks

	iconName, title string
	cwd             string

	tabstops *uv.TabStops

	pr *io.PipeReader
	pw *io.PipeWriter

	gl, gr  int
	gsingle int

	closed bool

	atPhantom bool
}

var _ Terminal = (*Emulator)(nil)

func NewEmulator(w, h int) *Emulator {
	t := new(Emulator)
	t.scrs[0] = *NewScreen(w, h)
	t.scrs[1] = *NewScreen(w, h)
	t.scr = &t.scrs[0]
	t.scrs[0].cb = &t.cb
	t.scrs[1].cb = &t.cb
	t.parser = ansi.NewParser()
	t.parser.SetParamsSize(parser.MaxParamsSize)
	t.parser.SetDataSize(1024 * 1024 * 4)
	t.parser.SetHandler(ansi.Handler{
		Print:     t.handlePrint,
		Execute:   t.handleControl,
		HandleCsi: t.handleCsi,
		HandleEsc: t.handleEsc,
		HandleDcs: t.handleDcs,
		HandleOsc: t.handleOsc,
		HandleApc: t.handleApc,
		HandlePm:  t.handlePm,
		HandleSos: t.handleSos,
	})
	t.pr, t.pw = io.Pipe()
	t.resetModes()
	t.tabstops = uv.DefaultTabStops(w)
	t.registerDefaultHandlers()

	t.defaultFg = color.White
	t.defaultBg = color.Black
	t.defaultCur = color.White

	return t
}

func (e *Emulator) SetLogger(l Logger) {
	e.logger = l
}

func (e *Emulator) SetCallbacks(cb Callbacks) {
	e.cb = cb
	e.scrs[0].cb = &e.cb
	e.scrs[1].cb = &e.cb
}

func (e *Emulator) Touched() []*uv.LineData {
	return e.scr.Touched()
}

func (e *Emulator) String() string {
	s := e.scr.buf.String()
	return uv.TrimSpace(s)
}

func (e *Emulator) Render() string {
	return e.scr.buf.Render()
}

var _ uv.Screen = (*Emulator)(nil)

func (e *Emulator) Bounds() uv.Rectangle {
	return e.scr.Bounds()
}

func (e *Emulator) CellAt(x, y int) *uv.Cell {
	return e.scr.CellAt(x, y)
}

func (e *Emulator) SetCell(x, y int, c *uv.Cell) {
	e.scr.SetCell(x, y, c)
}

func (e *Emulator) WidthMethod() uv.WidthMethod {
	if e.isModeSet(ansi.ModeUnicodeCore) {
		return ansi.GraphemeWidth
	}
	return ansi.WcWidth
}

func (e *Emulator) Draw(scr uv.Screen, area uv.Rectangle) {
	bg := uv.EmptyCell
	bg.Style.Bg = e.BackgroundColor()
	screen.FillArea(scr, &bg, area)
	for y := range e.Touched() {
		if y < 0 || y >= e.Height() {
			continue
		}
		for x := 0; x < e.Width(); {
			w := 1
			cell := e.CellAt(x, y)
			if cell != nil {
				cell = cell.Clone()
				if cell.Width > 1 {
					w = cell.Width
				}
				if cell.Style.Bg == nil && e.bgColor != nil {
					cell.Style.Bg = e.bgColor
				}
				if cell.Style.Fg == nil && e.fgColor != nil {
					cell.Style.Fg = e.fgColor
				}
				scr.SetCell(x+area.Min.X, y+area.Min.Y, cell)
			}
			x += w
		}
	}
}

func (e *Emulator) Height() int {
	return e.scr.Height()
}

func (e *Emulator) Width() int {
	return e.scr.Width()
}

func (e *Emulator) CursorPosition() uv.Position {
	x, y := e.scr.CursorPosition()
	return uv.Pos(x, y)
}

func (e *Emulator) Resize(width int, height int) {
	x, y := e.scr.CursorPosition()
	if e.atPhantom {
		if x < width-1 {
			e.atPhantom = false
			x++
		}
	}

	if y < 0 {
		y = 0
	}
	if y >= height {
		y = height - 1
	}
	if x < 0 {
		x = 0
	}
	if x >= width {
		x = width - 1
	}

	e.scrs[0].Resize(width, height)
	e.scrs[1].Resize(width, height)
	e.tabstops = uv.DefaultTabStops(width)

	e.setCursor(x, y)

	if e.isModeSet(ansi.ModeInBandResize) {
		_, _ = io.WriteString(e.pw, ansi.InBandResize(e.Height(), e.Width(), 0, 0))
	}
}

func (e *Emulator) Read(p []byte) (n int, err error) {
	if e.closed {
		return 0, io.EOF
	}

	return e.pr.Read(p) //nolint:wrapcheck
}

func (e *Emulator) Close() error {
	if e.closed {
		return nil
	}

	e.closed = true
	return e.pw.CloseWithError(io.EOF) //nolint:wrapcheck
}

func (e *Emulator) Write(p []byte) (n int, err error) {
	if e.closed {
		return 0, io.ErrClosedPipe
	}

	for i := range p {
		e.parser.Advance(p[i])
		state := e.parser.State()
		if len(e.grapheme) > 0 {
			if (e.lastState == parser.GroundState && state != parser.Utf8State) || i == len(p)-1 {
				e.flushGrapheme()
			}
		}
		e.lastState = state
	}
	return len(p), nil
}

func (e *Emulator) WriteString(s string) (n int, err error) {
	return e.Write([]byte(s))
}

func (e *Emulator) InputPipe() io.Writer {
	return e.pw
}

func (e *Emulator) Paste(text string) {
	if e.isModeSet(ansi.ModeBracketedPaste) {
		_, _ = io.WriteString(e.pw, ansi.BracketedPasteStart)
		defer io.WriteString(e.pw, ansi.BracketedPasteEnd) //nolint:errcheck
	}

	_, _ = io.WriteString(e.pw, text)
}

func (e *Emulator) SendText(text string) {
	_, _ = io.WriteString(e.pw, text)
}

func (e *Emulator) SendKeys(keys ...uv.KeyEvent) {
	for _, k := range keys {
		e.SendKey(k)
	}
}

func (e *Emulator) ForegroundColor() color.Color {
	if e.fgColor == nil {
		return e.defaultFg
	}
	return e.fgColor
}

func (e *Emulator) SetForegroundColor(c color.Color) {
	if c == nil {
		c = e.defaultFg
	}
	e.fgColor = c
	if e.cb.ForegroundColor != nil {
		e.cb.ForegroundColor(c)
	}
}

func (e *Emulator) SetDefaultForegroundColor(c color.Color) {
	if c == nil {
		c = color.White
	}
	e.defaultFg = c
}

func (e *Emulator) BackgroundColor() color.Color {
	if e.bgColor == nil {
		return e.defaultBg
	}
	return e.bgColor
}

func (e *Emulator) SetBackgroundColor(c color.Color) {
	if c == nil {
		c = e.defaultBg
	}
	e.bgColor = c
	if e.cb.BackgroundColor != nil {
		e.cb.BackgroundColor(c)
	}
}

func (e *Emulator) SetDefaultBackgroundColor(c color.Color) {
	if c == nil {
		c = color.Black
	}
	e.defaultBg = c
}

func (e *Emulator) CursorColor() color.Color {
	if e.curColor == nil {
		return e.defaultCur
	}
	return e.curColor
}

func (e *Emulator) SetCursorColor(c color.Color) {
	if c == nil {
		c = e.defaultCur
	}
	e.curColor = c
	if e.cb.CursorColor != nil {
		e.cb.CursorColor(c)
	}
}

func (e *Emulator) SetDefaultCursorColor(c color.Color) {
	if c == nil {
		c = color.White
	}
	e.defaultCur = c
}

func (e *Emulator) IndexedColor(i int) color.Color {
	if i < 0 || i > 255 {
		return nil
	}

	c := e.colors[i]
	if c == nil {
		return ansi.IndexedColor(i) //nolint:gosec
	}

	return c
}

func (e *Emulator) SetIndexedColor(i int, c color.Color) {
	if i < 0 || i > 255 {
		return
	}

	e.colors[i] = c
}

func (e *Emulator) resetTabStops() {
	e.tabstops = uv.DefaultTabStops(e.Width())
}

func (e *Emulator) logf(format string, v ...any) {
	if e.logger != nil {
		e.logger.Printf(format, v...)
	}
}

func (e *Emulator) Scrollback() *Scrollback {
	return e.scrs[0].Scrollback()
}

func (e *Emulator) ScrollbackLen() int {
	sb := e.Scrollback()
	if sb == nil {
		return 0
	}
	return sb.Len()
}

func (e *Emulator) ScrollbackCellAt(x, y int) *uv.Cell {
	sb := e.Scrollback()
	if sb == nil {
		return nil
	}
	return sb.CellAt(x, y)
}

func (e *Emulator) LineWrapped(y int) bool {
	return e.scr.LineWrapped(y)
}

func (e *Emulator) ScrollbackLineWrapped(y int) bool {
	sb := e.Scrollback()
	return sb != nil && sb.LineWrapped(y)
}

func (e *Emulator) SetScrollbackSize(maxLines int) {
	e.scrs[0].SetScrollbackSize(maxLines)
}

func (e *Emulator) ClearScrollback() {
	sb := e.Scrollback()
	if sb != nil {
		sb.Clear()
	}
}

func (e *Emulator) IsAltScreen() bool {
	return e.scr == &e.scrs[1]
}
