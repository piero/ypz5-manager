package main

import (
	"fmt"
	"path"

	tea "charm.land/bubbletea/v2"
)

// copyJob tracks a running copy. Each step runs as its own command, so
// the screen updates between files and the copy can be cancelled.
type copyJob struct {
	plan      copyPlan
	next      int
	copied    int
	written   int64
	skipped   []string
	errors    []string
	cancelled bool
	done      bool
}

type copyStepMsg struct {
	op      copyOp
	skipped string
	err     error
}

// nextCopyStep returns the command that performs the next step. Only
// that command touches the filesystem while the copy runs: the copy
// screen ignores everything that could read or write it.
func (m model) nextCopyStep() tea.Cmd {
	job := m.copy

	if job.cancelled || job.next >= len(job.plan.ops) {
		job.done = true
		job.skipped = append(job.plan.skipped, job.skipped...)

		return nil
	}

	fs := m.fs
	op := job.plan.ops[job.next]

	return func() tea.Msg {
		skipped, err := runCopyOp(fs, op)

		return copyStepMsg{op: op, skipped: skipped, err: err}
	}
}

func (m model) updateCopy(msg tea.Msg) (tea.Model, tea.Cmd) {
	job := m.copy

	switch msg := msg.(type) {
	case copyStepMsg:
		switch {
		case msg.err != nil:
			job.errors = append(job.errors, fmt.Sprintf("%s: %v", msg.op.dest, msg.err))

		case msg.skipped != "":
			job.skipped = append(job.skipped, msg.skipped)

		case !msg.op.isDir:
			job.copied++
			job.written += msg.op.size
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
	job := m.copy

	var s string

	s += "YPZ5 Manager\n\n"

	if !job.done {
		s += fmt.Sprintf("Copying to %s\n\n", m.path)
		s += fmt.Sprintf(
			"%s  %d/%d  %s of %s\n",
			progressBar(job.written, job.plan.bytes, 30),
			job.next,
			len(job.plan.ops),
			formatSize(job.written),
			formatSize(job.plan.bytes),
		)

		if job.next < len(job.plan.ops) {
			s += fmt.Sprintf("\n%s\n", path.Base(job.plan.ops[job.next].dest))
		}

		if job.cancelled {
			s += "\nCancelling after the current file...\n"
		} else {
			s += "\nEsc: cancel after the current file\n"
		}

		return s
	}

	switch {
	case job.cancelled:
		s += "Copy cancelled.\n\n"
	case len(job.errors) > 0:
		s += "Copy finished with errors.\n\n"
	default:
		s += "Copy finished.\n\n"
	}

	s += fmt.Sprintf(
		"Copied:  %d files (%s)\n",
		job.copied,
		formatSize(job.written),
	)

	s += listSection("Skipped", job.skipped)
	s += listSection("Errors", job.errors)

	s += "\nEnter: back\n"

	return s
}

// listSection renders a titled list, showing at most 10 items.
func listSection(title string, items []string) string {
	if len(items) == 0 {
		return ""
	}

	const limit = 10

	s := fmt.Sprintf("\n%s (%d):\n", title, len(items))

	for i, item := range items {
		if i == limit {
			s += fmt.Sprintf("  ... and %d more\n", len(items)-limit)
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

	bar := make([]rune, width)

	for i := range bar {
		if i < filled {
			bar[i] = '█'
		} else {
			bar[i] = '░'
		}
	}

	return "[" + string(bar) + "]"
}
