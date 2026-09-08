package vt

import (
	"image/color"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
)

type SafeEmulator struct {
	*Emulator
	mu sync.RWMutex
}

var _ Terminal = (*SafeEmulator)(nil)

func NewSafeEmulator(w, h int) *SafeEmulator {
	return &SafeEmulator{
		Emulator: NewEmulator(w, h),
	}
}

func (se *SafeEmulator) Write(data []byte) (int, error) {
	se.mu.Lock()
	defer se.mu.Unlock()
	return se.Emulator.Write(data)
}

func (se *SafeEmulator) Read(p []byte) (int, error) {
	return se.Emulator.Read(p)
}

func (se *SafeEmulator) Resize(w, h int) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.Resize(w, h)
}

func (se *SafeEmulator) Render() string {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.Render()
}

func (se *SafeEmulator) SetCell(x, y int, cell *uv.Cell) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SetCell(x, y, cell)
}

func (se *SafeEmulator) CellAt(x, y int) *uv.Cell {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.CellAt(x, y)
}

func (se *SafeEmulator) SendKey(key uv.KeyEvent) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SendKey(key)
}

func (se *SafeEmulator) SendMouse(mouse uv.MouseEvent) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SendMouse(mouse)
}

func (se *SafeEmulator) SendText(text string) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SendText(text)
}

func (se *SafeEmulator) Paste(text string) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.Paste(text)
}

func (se *SafeEmulator) SetForegroundColor(color color.Color) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SetForegroundColor(color)
}

func (se *SafeEmulator) SetBackgroundColor(color color.Color) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SetBackgroundColor(color)
}

func (se *SafeEmulator) SetCursorColor(color color.Color) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SetCursorColor(color)
}

func (se *SafeEmulator) SetIndexedColor(index int, color color.Color) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SetIndexedColor(index, color)
}

func (se *SafeEmulator) IndexedColor(index int) color.Color {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.IndexedColor(index)
}

func (se *SafeEmulator) Touched() []*uv.LineData {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.Touched()
}

func (se *SafeEmulator) Height() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.Height()
}

func (se *SafeEmulator) Width() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.Width()
}

func (se *SafeEmulator) ForegroundColor() color.Color {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.ForegroundColor()
}

func (se *SafeEmulator) BackgroundColor() color.Color {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.BackgroundColor()
}

func (se *SafeEmulator) CursorColor() color.Color {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.CursorColor()
}

func (se *SafeEmulator) CursorPosition() uv.Position {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.CursorPosition()
}

func (se *SafeEmulator) Draw(s uv.Screen, a uv.Rectangle) {
	se.mu.RLock()
	defer se.mu.RUnlock()
	se.Emulator.Draw(s, a)
}

func (se *SafeEmulator) Scrollback() *Scrollback {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.Scrollback()
}

func (se *SafeEmulator) ScrollbackLen() int {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.ScrollbackLen()
}

func (se *SafeEmulator) ScrollbackCellAt(x, y int) *uv.Cell {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.ScrollbackCellAt(x, y)
}

func (se *SafeEmulator) SetScrollbackSize(maxLines int) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.SetScrollbackSize(maxLines)
}

func (se *SafeEmulator) ClearScrollback() {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.Emulator.ClearScrollback()
}

func (se *SafeEmulator) IsAltScreen() bool {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.Emulator.IsAltScreen()
}
