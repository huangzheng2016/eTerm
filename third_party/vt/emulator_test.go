package vt

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

type testLogger struct {
	t testing.TB
}

func (l *testLogger) Printf(format string, v ...any) {
	l.t.Logf(format, v...)
}

func newTestTerminal(t testing.TB, width, height int) *Emulator {
	term := NewEmulator(width, height)
	term.SetLogger(&testLogger{t})
	return term
}

var cases = []struct {
	name  string
	w, h  int
	input []string
	want  []string
	pos   uv.Position
}{
	{
		name: "CBT Left Beyond First Column",
		w:    10, h: 1,
		input: []string{
			"\x1b[?W",
			"\x1b[10Z",
			"A",
		},
		want: []string{"A         "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "CBT Left Starting After Tab Stop",
		w:    11, h: 1,
		input: []string{
			"\x1b[?W",
			"\x1b[1;10H",
			"X",
			"\x1b[Z",
			"A",
		},
		want: []string{"        AX "},
		pos:  uv.Pos(9, 0),
	},
	{
		name: "CBT Left Starting on Tabstop",
		w:    10, h: 1,
		input: []string{
			"\x1b[?W",
			"\x1b[1;9H",
			"X",
			"\x1b[1;9H",
			"\x1b[Z",
			"A",
		},
		want: []string{"A       X "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "CBT Left Margin with Origin Mode",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?W",
			"\x1b[?6h",
			"\x1b[?69h",
			"\x1b[3;6s",
			"\x1b[1;2H",
			"X",
			"\x1b[Z",
			"A",
		},
		want: []string{"  AX      "},
		pos:  uv.Pos(3, 0),
	},

	{
		name: "CHT Right Beyond Last Column",
		w:    10, h: 1,
		input: []string{
			"\x1b[?W",
			"\x1b[100I",
			"A",
		},
		want: []string{"         A"},
		pos:  uv.Pos(9, 0),
	},
	{
		name: "CHT Right From Before Tabstop",
		w:    10, h: 1,
		input: []string{
			"\x1b[?W",
			"\x1b[1;2H",
			"A",
			"\x1b[I",
			"X",
		},
		want: []string{" A      X "},
		pos:  uv.Pos(9, 0),
	},
	{
		name: "CHT Right Margin",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?W",
			"\x1b[?69h",
			"\x1b[3;6s",
			"\x1b[1;1H",
			"X",
			"\x1b[I",
			"A",
		},
		want: []string{"X    A    "},
		pos:  uv.Pos(6, 0),
	},

	{
		name: "CR Pending Wrap is Unset",
		w:    10, h: 2,
		input: []string{
			"\x1b[10G",
			"A",
			"\r",
			"X",
		},
		want: []string{
			"X        A",
			"          ",
		},
		pos: uv.Pos(1, 0),
	},
	{
		name: "CR Left Margin",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[2;5s",
			"\x1b[4G",
			"A",
			"\r",
			"X",
		},
		want: []string{" X A      "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "CR Left of Left Margin",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[2;5s",
			"\x1b[4G",
			"A",
			"\x1b[1G",
			"\r",
			"X",
		},
		want: []string{"X  A      "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "CR Left Margin with Origin Mode",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?6h",
			"\x1b[?69h",
			"\x1b[2;5s",
			"\x1b[4G",
			"A",
			"\x1b[1G",
			"\r",
			"X",
		},
		want: []string{" X A      "},
		pos:  uv.Pos(2, 0),
	},

	{
		name: "CUB Pending Wrap is Unset",
		w:    10, h: 2,
		input: []string{
			"\x1b[10G",
			"A",
			"\x1b[D",
			"XYZ",
		},
		want: []string{
			"        XY",
			"Z         ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "CUB Leftmost Boundary with Reverse Wrap Disabled",
		w:    10, h: 2,
		input: []string{
			"\x1b[?45l",
			"A\n",
			"\x1b[10D",
			"B",
		},
		want: []string{
			"A         ",
			"B         ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "CUB Reverse Wrap",
		w:    10, h: 2,
		input: []string{
			"\x1b[?7h",
			"\x1b[?45h",
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[10G",
			"AB",
			"\x1b[D",
			"X",
		},
		want: []string{
			"         A",
			"X         ",
		},
		pos: uv.Pos(1, 1),
	},

	{
		name: "CUD Cursor Down",
		w:    10, h: 3,
		input: []string{
			"A",
			"\x1b[2B",
			"X",
		},
		want: []string{
			"A         ",
			"          ",
			" X        ",
		},
		pos: uv.Pos(2, 2),
	},
	{
		name: "CUD Cursor Down Above Bottom Margin",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\n\n\n\n",
			"\x1b[1;3r",
			"A",
			"\x1b[5B",
			"X",
		},
		want: []string{
			"A         ",
			"          ",
			" X        ",
			"          ",
		},
		pos: uv.Pos(2, 2),
	},
	{
		name: "CUD Cursor Down Below Bottom Margin",
		w:    10, h: 5,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\n\n\n\n\n",
			"\x1b[1;3r",
			"A",
			"\x1b[4;1H",
			"\x1b[5B",
			"X",
		},
		want: []string{
			"A         ",
			"          ",
			"          ",
			"          ",
			"X         ",
		},
		pos: uv.Pos(1, 4),
	},

	{
		name: "CUP Normal Usage",
		w:    10, h: 2,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[2;3H",
			"A",
		},
		want: []string{
			"          ",
			"  A       ",
		},
		pos: uv.Pos(3, 1),
	},
	{
		name: "CUP Off the Screen",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[500;500H",
			"A",
		},
		want: []string{
			"          ",
			"          ",
			"         A",
		},
		pos: uv.Pos(9, 2),
	},
	{
		name: "CUP Relative to Origin",
		w:    10, h: 2,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[2;3r",
			"\x1b[?6h",
			"\x1b[1;1H",
			"X",
		},
		want: []string{
			"          ",
			"X         ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "CUP Relative to Origin with Margins",
		w:    10, h: 2,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[3;5s",
			"\x1b[2;3r",
			"\x1b[?6h",
			"\x1b[1;1H",
			"X",
		},
		want: []string{
			"          ",
			"  X       ",
		},
		pos: uv.Pos(3, 1),
	},
	{
		name: "CUP Limits with Scroll Region and Origin Mode",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[3;5s",
			"\x1b[2;3r",
			"\x1b[?6h",
			"\x1b[500;500H",
			"X",
		},
		want: []string{
			"          ",
			"          ",
			"    X     ",
		},
		pos: uv.Pos(5, 2),
	},
	{
		name: "CUP Pending Wrap is Unset",
		w:    10, h: 1,
		input: []string{
			"\x1b[10G",
			"A",
			"\x1b[1;1H",
			"X",
		},
		want: []string{
			"X        A",
		},
		pos: uv.Pos(1, 0),
	},

	{
		name: "CUF Pending Wrap is Unset",
		w:    10, h: 2,
		input: []string{
			"\x1b[10G",
			"A",
			"\x1b[C",
			"XYZ",
		},
		want: []string{
			"         X",
			"YZ        ",
		},
		pos: uv.Pos(2, 1),
	},
	{
		name: "CUF Rightmost Boundary",
		w:    10, h: 1,
		input: []string{
			"A",
			"\x1b[500C",
			"B",
		},
		want: []string{
			"A        B",
		},
		pos: uv.Pos(9, 0),
	},
	{
		name: "CUF Left of Right Margin",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[3;5s",
			"\x1b[1G",
			"\x1b[500C",
			"X",
		},
		want: []string{
			"    X     ",
		},
		pos: uv.Pos(5, 0),
	},
	{
		name: "CUF Right of Right Margin",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[3;5s",
			"\x1b[6G",
			"\x1b[500C",
			"X",
		},
		want: []string{
			"         X",
		},
		pos: uv.Pos(9, 0),
	},

	{
		name: "CUU Normal Usage",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[3;1H",
			"A",
			"\x1b[2A",
			"X",
		},
		want: []string{
			" X        ",
			"          ",
			"A         ",
		},
		pos: uv.Pos(2, 0),
	},
	{
		name: "CUU Below Top Margin",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[2;4r",
			"\x1b[3;1H",
			"A",
			"\x1b[5A",
			"X",
		},
		want: []string{
			"          ",
			" X        ",
			"A         ",
			"          ",
		},
		pos: uv.Pos(2, 1),
	},
	{
		name: "CUU Above Top Margin",
		w:    10, h: 5,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[3;5r",
			"\x1b[3;1H",
			"A",
			"\x1b[2;1H",
			"\x1b[5A",
			"X",
		},
		want: []string{
			"X         ",
			"          ",
			"A         ",
			"          ",
			"          ",
		},
		pos: uv.Pos(1, 0),
	},

	{
		name: "DL Simple Delete Line",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[M",
		},
		want: []string{
			"ABC     ",
			"GHI     ",
			"        ",
		},
		pos: uv.Pos(0, 1),
	},
	{
		name: "DL Cursor Outside Scroll Region",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[3;4r",
			"\x1b[2;2H",
			"\x1b[M",
		},
		want: []string{
			"ABC     ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "DL With Top/Bottom Scroll Regions",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI\r\n",
			"123",
			"\x1b[1;3r",
			"\x1b[2;2H",
			"\x1b[M",
		},
		want: []string{
			"ABC     ",
			"GHI     ",
			"        ",
			"123     ",
		},
		pos: uv.Pos(0, 1),
	},
	{
		name: "DL With Left/Right Scroll Regions",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC123\r\n",
			"DEF456\r\n",
			"GHI789",
			"\x1b[?69h",
			"\x1b[2;4s",
			"\x1b[2;2H",
			"\x1b[M",
		},
		want: []string{
			"ABC123  ",
			"DHI756  ",
			"G   89  ",
		},
		pos: uv.Pos(1, 1),
	},

	{
		name: "IL Simple Insert Line",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[L",
		},
		want: []string{
			"ABC     ",
			"        ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(0, 1),
	},
	{
		name: "IL Cursor Outside Scroll Region",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[3;4r",
			"\x1b[2;2H",
			"\x1b[L",
		},
		want: []string{
			"ABC     ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "IL With Top/Bottom Scroll Regions",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI\r\n",
			"123",
			"\x1b[1;3r",
			"\x1b[2;2H",
			"\x1b[L",
		},
		want: []string{
			"ABC     ",
			"        ",
			"DEF     ",
			"123     ",
		},
		pos: uv.Pos(0, 1),
	},
	{
		name: "IL With Left/Right Scroll Regions",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC123\r\n",
			"DEF456\r\n",
			"GHI789",
			"\x1b[?69h",
			"\x1b[2;4s",
			"\x1b[2;2H",
			"\x1b[L",
		},
		want: []string{
			"ABC123  ",
			"D   56  ",
			"GEF489  ",
			" HI7    ",
		},
		pos: uv.Pos(1, 1),
	},

	{
		name: "DCH Simple Delete Character",
		w:    8, h: 1,
		input: []string{
			"ABC123",
			"\x1b[3G",
			"\x1b[2P",
		},
		want: []string{"AB23    "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "DCH with SGR State",
		w:    8, h: 1,
		input: []string{
			"ABC123",
			"\x1b[3G",
			"\x1b[41m",
			"\x1b[2P",
		},
		want: []string{"AB23    "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "DCH Outside Left/Right Scroll Region",
		w:    8, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC123",
			"\x1b[?69h",
			"\x1b[3;5s",
			"\x1b[2G",
			"\x1b[P",
		},
		want: []string{"ABC123  "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "DCH Inside Left/Right Scroll Region",
		w:    8, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC123",
			"\x1b[?69h",
			"\x1b[3;5s",
			"\x1b[4G",
			"\x1b[P",
		},
		want: []string{"ABC2 3  "},
		pos:  uv.Pos(3, 0),
	},
	{
		name: "DCH Split Wide Character",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"A橋123",
			"\x1b[3G",
			"\x1b[P",
		},
		want: []string{"A 123     "},
		pos:  uv.Pos(2, 0),
	},

	{
		name: "DECSTBM Full Screen Scroll Up",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[r",
			"\x1b[T",
		},
		want: []string{
			"        ",
			"ABC     ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "DECSTBM Top Only Scroll Up",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2r",
			"\x1b[T",
		},
		want: []string{
			"ABC     ",
			"        ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "DECSTBM Top and Bottom Scroll Up",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[1;2r",
			"\x1b[T",
		},
		want: []string{
			"        ",
			"ABC     ",
			"GHI     ",
			"        ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "DECSTBM Top Equal Bottom Scroll Up",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2r",
			"\x1b[T",
		},
		want: []string{
			"        ",
			"ABC     ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(3, 2),
	},

	{
		name: "DECSLRM Full Screen",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[?69h",
			"\x1b[s",
			"\x1b[X",
		},
		want: []string{
			" BC     ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "DECSLRM Left Only",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[?69h",
			"\x1b[2s",
			"\x1b[2G",
			"\x1b[L",
		},
		want: []string{
			"A       ",
			"DBC     ",
			"GEF     ",
			" HI     ",
		},
		pos: uv.Pos(1, 0),
	},
	{
		name: "DECSLRM Left And Right",
		w:    8, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[?69h",
			"\x1b[1;2s",
			"\x1b[2G",
			"\x1b[L",
		},
		want: []string{
			"  C     ",
			"ABF     ",
			"DEI     ",
			"GH      ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "DECSLRM Left Equal to Right",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[?69h",
			"\x1b[2;2s",
			"\x1b[X",
		},
		want: []string{
			"ABC     ",
			"DEF     ",
			"GHI     ",
		},
		pos: uv.Pos(3, 2),
	},

	{
		name: "ECH Simple Operation",
		w:    8, h: 1,
		input: []string{
			"ABC",
			"\x1b[1G",
			"\x1b[2X",
		},
		want: []string{"  C     "},
		pos:  uv.Pos(0, 0),
	},
	{
		name: "ECH Erasing Beyond Edge of Screen",
		w:    8, h: 1,
		input: []string{
			"\x1b[8G",
			"\x1b[2D",
			"ABC",
			"\x1b[D",
			"\x1b[10X",
		},
		want: []string{"     A  "},
		pos:  uv.Pos(6, 0),
	},
	{
		name: "ECH Reset Pending Wrap State",
		w:    8, h: 1,
		input: []string{
			"\x1b[8G",
			"A",
			"\x1b[X",
			"X",
		},
		want: []string{"       X"},
		pos:  uv.Pos(7, 0),
	},
	{
		name: "ECH with SGR State",
		w:    8, h: 1,
		input: []string{
			"ABC",
			"\x1b[1G",
			"\x1b[41m",
			"\x1b[2X",
		},
		want: []string{"  C     "},
		pos:  uv.Pos(0, 0),
	},
	{
		name: "ECH Multi-cell Character",
		w:    8, h: 1,
		input: []string{
			"橋BC",
			"\x1b[1G",
			"\x1b[X",
			"X",
		},
		want: []string{"X BC    "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "ECH Left/Right Scroll Region Ignored",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[1;3s",
			"\x1b[4G",
			"ABC",
			"\x1b[1G",
			"\x1b[4X",
		},
		want: []string{"    BC    "},
		pos:  uv.Pos(0, 0),
	},

	{
		name: "EL Simple Erase Right",
		w:    8, h: 1,
		input: []string{
			"ABCDE",
			"\x1b[3G",
			"\x1b[0K",
		},
		want: []string{"AB      "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "EL Erase Right Resets Pending Wrap",
		w:    8, h: 1,
		input: []string{
			"\x1b[8G",
			"A",
			"\x1b[0K",
			"X",
		},
		want: []string{"       X"},
		pos:  uv.Pos(7, 0),
	},
	{
		name: "EL Erase Right with SGR State",
		w:    8, h: 1,
		input: []string{
			"ABC",
			"\x1b[2G",
			"\x1b[41m",
			"\x1b[0K",
		},
		want: []string{"A       "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "EL Erase Right Multi-cell Character",
		w:    8, h: 1,
		input: []string{
			"AB橋DE",
			"\x1b[4G",
			"\x1b[0K",
		},
		want: []string{"AB      "},
		pos:  uv.Pos(3, 0),
	},
	{
		name: "EL Erase Right with Left/Right Margins",
		w:    10, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABCDE",
			"\x1b[?69h",
			"\x1b[1;3s",
			"\x1b[2G",
			"\x1b[0K",
		},
		want: []string{"A         "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "EL Simple Erase Left",
		w:    8, h: 1,
		input: []string{
			"ABCDE",
			"\x1b[3G",
			"\x1b[1K",
		},
		want: []string{"   DE   "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "EL Erase Left with SGR State",
		w:    8, h: 1,
		input: []string{
			"ABC",
			"\x1b[2G",
			"\x1b[41m",
			"\x1b[1K",
		},
		want: []string{"  C     "},
		pos:  uv.Pos(1, 0),
	},
	{
		name: "EL Erase Left Multi-cell Character",
		w:    8, h: 1,
		input: []string{
			"AB橋DE",
			"\x1b[3G",
			"\x1b[1K",
		},
		want: []string{"    DE  "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "EL Simple Erase Complete Line",
		w:    8, h: 1,
		input: []string{
			"ABCDE",
			"\x1b[3G",
			"\x1b[2K",
		},
		want: []string{"        "},
		pos:  uv.Pos(2, 0),
	},
	{
		name: "EL Erase Complete with SGR State",
		w:    8, h: 1,
		input: []string{
			"ABC",
			"\x1b[2G",
			"\x1b[41m",
			"\x1b[2K",
		},
		want: []string{"        "},
		pos:  uv.Pos(1, 0),
	},

	{
		name: "IND No Scroll Region Top of Screen",
		w:    10, h: 2,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"A",
			"\x1bD",
			"X",
		},
		want: []string{
			"A         ",
			" X        ",
		},
		pos: uv.Pos(2, 1),
	},
	{
		name: "IND Bottom of Primary Screen",
		w:    10, h: 2,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[2;1H",
			"A",
			"\x1bD",
			"X",
		},
		want: []string{
			"A         ",
			" X        ",
		},
		pos: uv.Pos(2, 1),
	},
	{
		name: "IND Inside Scroll Region",
		w:    10, h: 2,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[1;3r",
			"A",
			"\x1bD",
			"X",
		},
		want: []string{
			"A         ",
			" X        ",
		},
		pos: uv.Pos(2, 1),
	},
	{
		name: "IND Bottom of Scroll Region",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[1;3r",
			"\x1b[4;1H",
			"B",
			"\x1b[3;1H",
			"A",
			"\x1bD",
			"X",
		},
		want: []string{
			"          ",
			"A         ",
			" X        ",
			"B         ",
		},
		pos: uv.Pos(2, 2),
	},
	{
		name: "IND Bottom of Primary Screen with Scroll Region",
		w:    10, h: 5,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[1;3r",
			"\x1b[3;1H",
			"A",
			"\x1b[5;1H",
			"\x1bD",
			"X",
		},
		want: []string{
			"          ",
			"          ",
			"A         ",
			"          ",
			"X         ",
		},
		pos: uv.Pos(1, 4),
	},
	{
		name: "IND Outside of Left/Right Scroll Region",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?69h",
			"\x1b[1;3r",
			"\x1b[3;5s",
			"\x1b[3;3H",
			"A",
			"\x1b[3;1H",
			"\x1bD",
			"X",
		},
		want: []string{
			"          ",
			"          ",
			"X A       ",
		},
		pos: uv.Pos(1, 2),
	},
	{
		name: "IND Inside of Left/Right Scroll Region",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"AAAAAA\r\n",
			"AAAAAA\r\n",
			"AAAAAA",
			"\x1b[?69h",
			"\x1b[1;3s",
			"\x1b[1;3r",
			"\x1b[3;1H",
			"\x1bD",
		},
		want: []string{
			"AAAAAA    ",
			"AAAAAA    ",
			"   AAA    ",
		},
		pos: uv.Pos(0, 2),
	},

	{
		name: "ED Simple Erase Below",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[0J",
		},
		want: []string{
			"ABC     ",
			"D       ",
			"        ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "ED Erase Below with SGR State",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[0J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[41m",
			"\x1b[0J",
		},
		want: []string{
			"ABC     ",
			"D       ",
			"        ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "ED Erase Below with Multi-Cell Character",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"AB橋C\r\n",
			"DE橋F\r\n",
			"GH橋I",
			"\x1b[2;3H",
			"\x1b[0J",
		},
		want: []string{
			"AB橋C   ",
			"DE      ",
			"        ",
		},
		pos: uv.Pos(2, 1),
	},
	{
		name: "ED Simple Erase Above",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[1J",
		},
		want: []string{
			"        ",
			"        ",
			"GHI     ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "ED Simple Erase Complete",
		w:    8, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[2J",
		},
		want: []string{
			"        ",
			"        ",
			"        ",
		},
		pos: uv.Pos(1, 1),
	},

	{
		name: "RI No Scroll Region Top of Screen",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"A\r\n",
			"B\r\n",
			"C\r\n",
			"\x1b[1;1H",
			"\x1bM",
			"X",
		},
		want: []string{
			"X         ",
			"A         ",
			"B         ",
			"C         ",
		},
		pos: uv.Pos(1, 0),
	},
	{
		name: "RI No Scroll Region Not Top of Screen",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"A\r\n",
			"B\r\n",
			"C",
			"\x1b[2;1H",
			"\x1bM",
			"X",
		},
		want: []string{
			"X         ",
			"B         ",
			"C         ",
		},
		pos: uv.Pos(1, 0),
	},
	{
		name: "RI Top/Bottom Scroll Region",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"A\r\n",
			"B\r\n",
			"C",
			"\x1b[2;3r",
			"\x1b[2;1H",
			"\x1bM",
			"X",
		},
		want: []string{
			"A         ",
			"X         ",
			"B         ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "RI Outside of Top/Bottom Scroll Region",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"A\r\n",
			"B\r\n",
			"C",
			"\x1b[2;3r",
			"\x1b[1;1H",
			"\x1bM",
		},
		want: []string{
			"A         ",
			"B         ",
			"C         ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "RI Left/Right Scroll Region",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[?69h",
			"\x1b[2;3s",
			"\x1b[1;2H",
			"\x1bM",
		},
		want: []string{
			"A         ",
			"DBC       ",
			"GEF       ",
			" HI       ",
		},
		pos: uv.Pos(1, 0),
	},
	{
		name: "RI Outside Left/Right Scroll Region",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[?69h",
			"\x1b[2;3s",
			"\x1b[2;1H",
			"\x1bM",
		},
		want: []string{
			"ABC       ",
			"DEF       ",
			"GHI       ",
		},
		pos: uv.Pos(0, 0),
	},

	{
		name: "SD Outside of Top/Bottom Scroll Region",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[3;4r",
			"\x1b[2;2H",
			"\x1b[T",
		},
		want: []string{
			"ABC       ",
			"DEF       ",
			"          ",
			"GHI       ",
		},
		pos: uv.Pos(1, 1),
	},

	{
		name: "SU Simple Usage",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;2H",
			"\x1b[S",
		},
		want: []string{
			"DEF       ",
			"GHI       ",
			"          ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "SU Top/Bottom Scroll Region",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC\r\n",
			"DEF\r\n",
			"GHI",
			"\x1b[2;3r",
			"\x1b[1;1H",
			"\x1b[S",
		},
		want: []string{
			"ABC       ",
			"GHI       ",
			"          ",
		},
		pos: uv.Pos(0, 0),
	},
	{
		name: "SU Left/Right Scroll Regions",
		w:    10, h: 3,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"ABC123\r\n",
			"DEF456\r\n",
			"GHI789",
			"\x1b[?69h",
			"\x1b[2;4s",
			"\x1b[2;2H",
			"\x1b[S",
		},
		want: []string{
			"AEF423    ",
			"DHI756    ",
			"G   89    ",
		},
		pos: uv.Pos(1, 1),
	},
	{
		name: "SU Preserves Pending Wrap",
		w:    10, h: 4,
		input: []string{
			"\x1b[1;10H",
			"\x1b[2J",
			"A",
			"\x1b[2;10H",
			"B",
			"\x1b[3;10H",
			"C",
			"\x1b[S",
			"X",
		},
		want: []string{
			"         B",
			"         C",
			"          ",
			"X         ",
		},
		pos: uv.Pos(1, 3),
	},
	{
		name: "SU Scroll Full Top/Bottom Scroll Region",
		w:    10, h: 5,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"top",
			"\x1b[5;1H",
			"ABCDEF",
			"\x1b[2;5r",
			"\x1b[4S",
		},
		want: []string{
			"top       ",
			"          ",
			"          ",
			"          ",
			"          ",
		},
		pos: uv.Pos(0, 0),
	},

	{
		name: "TBC Clear Single Tab Stop",
		w:    23, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?W",
			"\t",
			"\x1b[g",
			"\x1b[1G",
			"\t",
		},
		want: []string{"                       "},
		pos:  uv.Pos(16, 0),
	},
	{
		name: "TBC Clear All Tab Stops",
		w:    23, h: 1,
		input: []string{
			"\x1b[1;1H",
			"\x1b[2J",
			"\x1b[?W",
			"\x1b[3g",
			"\x1b[1G",
			"\t",
		},
		want: []string{"                       "},
		pos:  uv.Pos(22, 0),
	},
}

func TestTerminal(t *testing.T) {
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			term := newTestTerminal(t, tt.w, tt.h)
			for _, in := range tt.input {
				term.Write([]byte(in))
			}
			got := termText(term)
			if len(got) != len(tt.want) {
				t.Errorf("output length doesn't match: want %d, got %d", len(tt.want), len(got))
			}
			for i := 0; i < len(got) && i < len(tt.want); i++ {
				if got[i] != tt.want[i] {
					t.Errorf("line %d doesn't match:\nwant: %q\ngot:  %q", i+1, tt.want[i], got[i])
				}
			}
			pos := term.CursorPosition()
			if pos != tt.pos {
				t.Errorf("cursor position doesn't match: want %v, got %v", tt.pos, pos)
			}
		})
	}
}

func termText(term *Emulator) []string {
	var lines []string
	for y := range term.Height() {
		var line string
		for x := 0; x < term.Width(); x++ {
			cell := term.CellAt(x, y)
			if cell == nil {
				continue
			}
			line += cell.String()
			x += cell.Width - 1
		}
		lines = append(lines, line)
	}
	return lines
}
