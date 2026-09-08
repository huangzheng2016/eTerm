package vt

import uv "github.com/charmbracelet/ultraviolet"

type Damage interface {
	Bounds() uv.Rectangle
}

type CellDamage struct {
	X, Y  int
	Width int
}

func (d CellDamage) Bounds() uv.Rectangle {
	return uv.Rect(d.X, d.Y, d.Width, 1)
}

type RectDamage uv.Rectangle

func (d RectDamage) Bounds() uv.Rectangle {
	return uv.Rectangle(d)
}

func (d RectDamage) X() int {
	return uv.Rectangle(d).Min.X
}

func (d RectDamage) Y() int {
	return uv.Rectangle(d).Min.Y
}

func (d RectDamage) Width() int {
	return uv.Rectangle(d).Dx()
}

func (d RectDamage) Height() int {
	return uv.Rectangle(d).Dy()
}

type ScreenDamage struct {
	Width, Height int
}

func (d ScreenDamage) Bounds() uv.Rectangle {
	return uv.Rect(0, 0, d.Width, d.Height)
}

type MoveDamage struct {
	Src, Dst uv.Rectangle
}

type ScrollDamage struct {
	uv.Rectangle
	Dx, Dy int
}
