package vt

import uv "github.com/charmbracelet/ultraviolet"

type Buffer struct {
	uv.Buffer
}

func (b *Buffer) InsertLine(y, n int, c *uv.Cell) {
	b.InsertLineRect(y, n, c, b.Bounds())
}

func (b *Buffer) InsertLineRect(y, n int, c *uv.Cell, rect uv.Rectangle) {
	if n <= 0 || y < rect.Min.Y || y >= rect.Max.Y || y >= b.Height() {
		return
	}

	if y+n > rect.Max.Y {
		n = rect.Max.Y - y
	}

	for i := rect.Max.Y - 1; i >= y+n; i-- {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			b.Lines[i][x] = b.Lines[i-n][x]
		}
	}

	for i := y; i < y+n; i++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

func (b *Buffer) DeleteLineRect(y, n int, c *uv.Cell, rect uv.Rectangle) {
	if n <= 0 || y < rect.Min.Y || y >= rect.Max.Y || y >= b.Height() {
		return
	}

	if n > rect.Max.Y-y {
		n = rect.Max.Y - y
	}

	for dst := y; dst < rect.Max.Y-n; dst++ {
		src := dst + n
		for x := rect.Min.X; x < rect.Max.X; x++ {
			b.Lines[dst][x] = b.Lines[src][x]
		}
	}

	for i := rect.Max.Y - n; i < rect.Max.Y; i++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

func (b *Buffer) DeleteLine(y, n int, c *uv.Cell) {
	b.DeleteLineRect(y, n, c, b.Bounds())
}

func (b *Buffer) InsertCell(x, y, n int, c *uv.Cell) {
	b.InsertCellRect(x, y, n, c, b.Bounds())
}

func (b *Buffer) InsertCellRect(x, y, n int, c *uv.Cell, rect uv.Rectangle) {
	if n <= 0 || y < rect.Min.Y || y >= rect.Max.Y || y >= b.Height() ||
		x < rect.Min.X || x >= rect.Max.X || x >= b.Width() {
		return
	}

	if x+n > rect.Max.X {
		n = rect.Max.X - x
	}

	for i := rect.Max.X - 1; i >= x+n && i-n >= rect.Min.X; i-- {
		b.Lines[y][i] = b.Lines[y][i-n]
	}

	for i := x; i < x+n && i < rect.Max.X; i++ {
		b.SetCell(i, y, c)
	}
}

func (b *Buffer) DeleteCell(x, y, n int, c *uv.Cell) {
	b.DeleteCellRect(x, y, n, c, b.Bounds())
}

func (b *Buffer) DeleteCellRect(x, y, n int, c *uv.Cell, rect uv.Rectangle) {
	if n <= 0 || y < rect.Min.Y || y >= rect.Max.Y || y >= b.Height() ||
		x < rect.Min.X || x >= rect.Max.X || x >= b.Width() {
		return
	}

	remainingCells := rect.Max.X - x
	if n > remainingCells {
		n = remainingCells
	}

	for i := x; i < rect.Max.X-n; i++ {
		if i+n < rect.Max.X {
			b.Lines[y][i] = b.Lines[y][i+n]
		}
	}

	for i := rect.Max.X - n; i < rect.Max.X; i++ {
		b.SetCell(i, y, c)
	}
}
