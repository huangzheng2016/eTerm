package ui

import (
	"charm.land/lipgloss/v2"
)

var (
	AppStyle = lipgloss.NewStyle().Padding(1, 2)

	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))

	SubtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))

	// Match charm bubbles list.DefaultStyles().Title (list header pill).
	ActiveTabStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	InactiveTabStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8a8a8a")).
		Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#333")).
			Foreground(lipgloss.Color("#fff")).
			Padding(0, 1)

	ErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)

	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))

	WarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)

	SelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

	DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#666"))

	PasswordMask rune = '*'
)
