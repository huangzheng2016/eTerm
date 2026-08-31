package aiview

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/huangzheng2016/eTerm/internal/ui"
)

const tasksRefreshInterval = time.Second

type tasksTickMsg struct{ seq int }

// openTasks switches to the tasks browser and starts the refresh tick.
func (m *Model) openTasks() tea.Cmd {
	m.refreshTasks()
	m.tCursor = 0
	m.mode = modeTasks
	m.tasksSeq++
	seq := m.tasksSeq
	return tea.Tick(tasksRefreshInterval, func(time.Time) tea.Msg { return tasksTickMsg{seq: seq} })
}

// tasksTick refreshes the list while the browser is open; the tick chain ends
// when the user leaves the view or reopens it (new seq).
func (m *Model) tasksTick(seq int) tea.Cmd {
	if seq != m.tasksSeq || (m.mode != modeTasks && m.mode != modeTaskDetail) {
		return nil
	}
	m.refreshTasks()
	return tea.Tick(tasksRefreshInterval, func(time.Time) tea.Msg { return tasksTickMsg{seq: seq} })
}

func (m *Model) refreshTasks() {
	ts, ok := m.runner.(interface{ Tasks() []TaskEntry })
	if !ok {
		return
	}
	m.taskList = ts.Tasks()
	if m.tCursor >= len(m.taskList) {
		m.tCursor = max(0, len(m.taskList)-1)
	}
}

func (m *Model) cancelTask(id string) {
	if c, ok := m.runner.(interface{ CancelTask(string) }); ok {
		c.CancelTask(id)
	}
	m.refreshTasks()
}

func (m *Model) detailTask() *TaskEntry {
	for i := range m.taskList {
		if m.taskList[i].ID == m.taskDetailID {
			return &m.taskList[i]
		}
	}
	return nil
}

func (m *Model) updateTasks(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeChat
	case "up", "k":
		if m.tCursor > 0 {
			m.tCursor--
		}
	case "down", "j":
		if m.tCursor < len(m.taskList)-1 {
			m.tCursor++
		}
	case "enter":
		if m.tCursor < len(m.taskList) {
			m.taskDetailID = m.taskList[m.tCursor].ID
			m.dOffset = 0
			m.mode = modeTaskDetail
		}
	case "x":
		if m.tCursor < len(m.taskList) && m.taskList[m.tCursor].Status == "running" {
			m.cancelTask(m.taskList[m.tCursor].ID)
		}
	}
	return nil
}

func (m *Model) updateTaskDetail(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeTasks
	case "up", "k":
		if m.dOffset > 0 {
			m.dOffset--
		}
	case "down", "j":
		m.dOffset++
	case "x":
		m.cancelTask(m.taskDetailID)
	}
	return nil
}

// activityText renders one tail entry; tool calls keep their prefix so they
// stand out from plain text snippets.
func activityText(a TaskActivity) string {
	if a.Kind == "tool" {
		return "tool: " + a.Text
	}
	return a.Text
}

func (m *Model) tasksView() string {
	cw := m.contentWidth()
	_, boxH, _, _ := m.layout()
	rows := []string{ui.TitleStyle.Render("Tasks"), ""}
	if len(m.taskList) == 0 {
		rows = append(rows, ui.DimStyle.Render("No background tasks."))
	} else {
		// Two rows per task; keep a window around the cursor so the list fits
		// the box (title+blank above, blank+hint below, inside the border).
		visible := max(1, (boxH-6)/2)
		start := 0
		if m.tCursor >= start+visible {
			start = m.tCursor - visible + 1
		}
		for i := start; i < min(len(m.taskList), start+visible); i++ {
			t := m.taskList[i]
			cursor := "  "
			style := ui.DimStyle
			if i == m.tCursor {
				cursor = "▸ "
				style = ui.SelectedStyle
			}
			head := fmt.Sprintf("%s [%s] %ds ago", t.ID, t.Status, t.StartedSecAgo)
			task := truncateCells(t.Task, max(0, cw-len(head)-4))
			rows = append(rows, truncateCells(cursor+style.Render(head)+" "+task, cw))
			last := ""
			if n := len(t.Tail); n > 0 {
				last = activityText(t.Tail[n-1])
			}
			rows = append(rows, truncateCells("    "+ui.DimStyle.Render(last), cw))
		}
	}
	rows = append(rows, "",
		ui.DimStyle.Render("enter inspect | x cancel | esc back"))
	return strings.Join(rows, "\n")
}

func (m *Model) taskDetailView() string {
	cw := m.contentWidth()
	_, boxH, _, _ := m.layout()
	title := ui.TitleStyle.Render("Task " + m.taskDetailID)
	rows := []string{title, ""}
	t := m.detailTask()
	if t == nil {
		rows = append(rows, ui.DimStyle.Render("task not found"))
	} else {
		rows[0] += ui.DimStyle.Render(fmt.Sprintf("  [%s] %ds ago", t.Status, t.StartedSecAgo))
		rows = append(rows, truncateCells(ui.DimStyle.Render(t.Task), cw), "")
		lines := make([]string, 0, len(t.Tail))
		for _, a := range t.Tail {
			lines = append(lines, truncateCells(activityText(a), cw))
		}
		if len(lines) == 0 {
			lines = append(lines, ui.DimStyle.Render("no activity yet"))
		}
		// Title+blank+task+blank above, blank+hint below, inside the border.
		visible := max(1, boxH-8)
		maxOff := max(0, len(lines)-visible)
		if m.dOffset > maxOff {
			m.dOffset = maxOff
		}
		rows = append(rows, lines[m.dOffset:min(len(lines), m.dOffset+visible)]...)
	}
	rows = append(rows, "",
		ui.DimStyle.Render("j/k scroll | x cancel | esc back"))
	return strings.Join(rows, "\n")
}
