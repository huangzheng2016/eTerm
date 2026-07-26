package sessionlistview

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"

	"github.com/huangzheng2016/eTerm/internal/ui/sshview"
)

type replayTickMsg struct {
	replay *replayState
	at     time.Time
}

type replayState struct {
	events   []sshview.ReplayEvent
	emu      *vt.Emulator
	next     int
	pos      time.Duration
	duration time.Duration
	playing  bool
	speed    float64
	lastTick time.Time
	jump     textinput.Model
	jumping  bool
}

func newReplayState(data []byte, duration time.Duration) (*replayState, error) {
	events, err := sshview.DecodeReplay(data)
	if err != nil {
		return nil, err
	}
	if duration <= 0 && len(events) > 0 {
		duration = time.Duration(events[len(events)-1].At) * time.Millisecond
	}
	j := textinput.New()
	j.Placeholder = "01:23:45, +30s, or -5m"
	j.SetWidth(28)
	r := &replayState{events: events, duration: duration, speed: 1, jump: j}
	r.reset()
	r.seek(0)
	return r, nil
}

func (r *replayState) reset() {
	r.closeEmulator()
	r.emu = vt.NewEmulator(80, 24)
	r.next = 0
	emu := r.emu
	go func() {
		_, _ = io.Copy(io.Discard, emu)
	}()
}

func (r *replayState) closeEmulator() {
	if r.emu == nil {
		return
	}
	if closer, ok := r.emu.InputPipe().(io.Closer); ok {
		_ = closer.Close()
	}
}

func (r *replayState) seek(pos time.Duration) {
	if pos < 0 {
		pos = 0
	}
	if pos > r.duration {
		pos = r.duration
	}
	if pos < r.pos {
		r.reset()
	}
	r.pos = pos
	for r.next < len(r.events) && time.Duration(r.events[r.next].At)*time.Millisecond <= pos {
		e := r.events[r.next]
		switch e.Kind {
		case "o":
			_, _ = r.emu.Write(e.Data)
		case "r":
			if e.Rows > 0 && e.Cols > 0 {
				r.emu.Resize(e.Cols, e.Rows)
			}
		}
		r.next++
	}
	if r.pos >= r.duration {
		r.playing = false
	}
}

func (r *replayState) tick(now time.Time) tea.Cmd {
	if r.lastTick.IsZero() {
		r.lastTick = now
	}
	if r.playing {
		delta := now.Sub(r.lastTick)
		r.seek(r.pos + time.Duration(float64(delta)*r.speed))
	}
	r.lastTick = now
	if !r.playing {
		return nil
	}
	return r.tickCmd()
}

func (r *replayState) tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return replayTickMsg{replay: r, at: t} })
}

func (r *replayState) toggle() tea.Cmd {
	if !r.playing && r.pos >= r.duration {
		r.seek(0)
	}
	r.playing = !r.playing
	r.lastTick = time.Now()
	if r.playing {
		return r.tickCmd()
	}
	return nil
}

func (r *replayState) changeSpeed(direction int) {
	speeds := []float64{0.5, 1, 2, 4, 8, 16}
	idx := 1
	for i, speed := range speeds {
		if speed == r.speed {
			idx = i
		}
	}
	idx = min(max(0, idx+direction), len(speeds)-1)
	r.speed = speeds[idx]
}

func parseReplayJump(value string, current time.Duration) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty time")
	}
	if value[0] == '+' || value[0] == '-' {
		d, err := time.ParseDuration(value[1:])
		if err != nil {
			return 0, err
		}
		if value[0] == '-' {
			d = -d
		}
		return current + d, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("invalid time")
	}
	var seconds int64
	for _, part := range parts {
		n, err := strconv.ParseInt(part, 10, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid time")
		}
		seconds = seconds*60 + n
	}
	return time.Duration(seconds) * time.Second, nil
}

func formatReplayTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, total/60%60, total%60)
}

func replayTimeline(pos, duration time.Duration, width int) string {
	width = max(10, width)
	filled := 0
	if duration > 0 {
		filled = int(float64(width-1) * float64(pos) / float64(duration))
	}
	filled = min(max(0, filled), width-1)
	return "[" + strings.Repeat("=", filled) + "|" + strings.Repeat("-", width-filled-1) + "]"
}
