package tui

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/piero/ypz5-manager/internal/transfer"
)

// copyJob tracks a running copy. Each step runs as its own command, so
// the screen updates between files and the copy can be cancelled.
type copyJob struct {
	plan      transfer.Plan
	dest      string
	next      int
	copied    int
	written   int64
	skipped   []string
	errors    []string
	cancelled bool
	done      bool
}

type copyStepMsg struct {
	op      transfer.Op
	skipped string
	err     error
}

// nextCopyStep returns the command that performs the next step. Only
// that command touches the filesystem while the copy runs: the copy
// screen ignores everything that could read or write it.
func (m model) nextCopyStep() tea.Cmd {
	job := m.copy

	if job.cancelled || job.next >= len(job.plan.Ops) {
		job.done = true
		job.skipped = append(job.plan.Skipped, job.skipped...)

		return nil
	}

	fs := m.fs
	op := job.plan.Ops[job.next]

	return func() tea.Msg {
		skipped, err := transfer.Run(fs, op)

		return copyStepMsg{op: op, skipped: skipped, err: err}
	}
}

func (m model) updateCopy(msg tea.Msg) (tea.Model, tea.Cmd) {
	job := m.copy

	switch msg := msg.(type) {
	case copyStepMsg:
		switch {
		case msg.err != nil:
			job.errors = append(job.errors, fmt.Sprintf("%s: %v", msg.op.Dest, msg.err))

		case msg.skipped != "":
			job.skipped = append(job.skipped, msg.skipped)

		case !msg.op.IsDir:
			job.copied++
			job.written += msg.op.Size
		}

		job.next++

		cmd := m.nextCopyStep()

		if job.done {
			entries, err := loadDirectory(m.fs, m.path)
			if err != nil {
				m.err = err
			} else {
				m.entries = entries
			}

			if m.cursor >= len(m.entries) {
				m.cursor = max(len(m.entries)-1, 0)
			}
		}

		return m, cmd

	case tea.KeyPressMsg:
		if job.done {
			switch msg.String() {
			case "enter", "esc", "q":
				m.copy = nil
			}

			return m, nil
		}

		// Quitting mid-write could cut a file short, so every way out
		// only cancels after the current file.
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			job.cancelled = true
		}
	}

	return m, nil
}

func (m model) copyView() string {
	p := devicePalette
	job := m.copy

	var s string

	if !job.done {
		s += p.titleBar("Copying from Mac") + "\n\n"
		s += p.label("Copying to", job.dest) + "\n\n"
		s += fmt.Sprintf(
			"%s  %d/%d  %s of %s\n",
			progressBar(job.written, job.plan.Bytes, 30),
			job.next,
			len(job.plan.Ops),
			transfer.FormatSize(job.written),
			transfer.FormatSize(job.plan.Bytes),
		)

		if job.next < len(job.plan.Ops) {
			s += "\n" + dimStyle.render(path.Base(job.plan.Ops[job.next].Dest)) + "\n"
		}

		if job.cancelled {
			s += "\n" + warningStyle.render("Cancelling after the current file...") + "\n"
		} else {
			s += "\n" + p.help("Esc", "cancel after the current file") + "\n"
		}

		return s
	}

	s += p.titleBar("Copy to "+job.dest) + "\n\n"

	switch {
	case job.cancelled:
		s += warningStyle.render("Copy cancelled.") + "\n\n"
	case len(job.errors) > 0:
		s += errorStyle.render("Copy finished with errors.") + "\n\n"
	default:
		s += successStyle.render("Copy finished.") + "\n\n"
	}

	s += fmt.Sprintf(
		"Copied:  %d files (%s)\n",
		job.copied,
		transfer.FormatSize(job.written),
	)

	s += listSection(warningStyle, "Skipped", job.skipped)
	s += listSection(errorStyle, "Errors", job.errors)

	s += "\n" + p.help("Enter", "back") + "\n"

	return s
}

// listSection renders a titled list, showing at most 10 items.
func listSection(titleStyle style, title string, items []string) string {
	if len(items) == 0 {
		return ""
	}

	const limit = 10

	s := "\n" + titleStyle.renderf("%s (%d):", title, len(items)) + "\n"

	for i, item := range items {
		if i == limit {
			s += dimStyle.renderf("  ... and %d more", len(items)-limit) + "\n"
			break
		}

		s += "  " + item + "\n"
	}

	return s
}

func progressBar(done, total int64, width int) string {
	filled := width

	if total > 0 {
		filled = int(done * int64(width) / total)
	}

	return "[" +
		devicePalette.accent.render(strings.Repeat("█", filled)) +
		dimStyle.render(strings.Repeat("░", width-filled)) +
		"]"
}
