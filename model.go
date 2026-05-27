package main

import (
	"fmt"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

// ─────────────────────────────────────────────────────────────────────────────
// Palette — charm-inspired, but tuned for fintech-neutral (Blossom-ish)
// ─────────────────────────────────────────────────────────────────────────────
var (
	colBrand  = lg.Color("#7C5CFF") // purple
	colAccent = lg.Color("#FF8A4C") // orange
	colOK     = lg.Color("#5FD068")
	colWarn   = lg.Color("#FFC857")
	colErr    = lg.Color("#FF5C5C")
	colMuted  = lg.Color("#8B8FA3")
	colDim    = lg.Color("#5A5E72")
	colFG     = lg.Color("#E8EAF0")
	colSelBG  = lg.Color("#2A2D3F")
)

var (
	styBrand  = lg.NewStyle().Foreground(colBrand).Bold(true)
	styAccent = lg.NewStyle().Foreground(colAccent)
	styMuted  = lg.NewStyle().Foreground(colMuted)
	styDim    = lg.NewStyle().Foreground(colDim)
	styOK     = lg.NewStyle().Foreground(colOK)
	styWarn   = lg.NewStyle().Foreground(colWarn)
	styErr    = lg.NewStyle().Foreground(colErr)
	styFG     = lg.NewStyle().Foreground(colFG)
	styBold   = lg.NewStyle().Foreground(colFG).Bold(true)
	stySel    = lg.NewStyle().Background(colSelBG).Foreground(colFG)
	styLogo   = lg.NewStyle().Foreground(colBrand).Bold(true).Padding(0, 1).Border(lg.RoundedBorder()).BorderForeground(colBrand)
)

// ─────────────────────────────────────────────────────────────────────────────
// Model
// ─────────────────────────────────────────────────────────────────────────────
type model struct {
	root        string
	depth       int
	force       bool
	includeMain bool

	items  []Worktree
	cursor int
	offset int

	scanning  bool
	scanned   int
	current   string
	scanCh    chan any
	startedAt time.Time
	elapsed   time.Duration

	width  int
	height int

	spin     int
	flashMsg string
	flashEnd time.Time
}

func newModel(root string, depth int, force, includeMain bool) *model {
	return &model{
		root:        root,
		depth:       depth,
		force:       force,
		includeMain: includeMain,
		width:       100,
		height:      30,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.startScanCmd(), tickCmd())
}

// ─────────────────────────────────────────────────────────────────────────────
// Commands
// ─────────────────────────────────────────────────────────────────────────────
type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// startScanCmd kicks off the scanner goroutine and returns a Cmd that
// reads the next message from the channel.
func (m *model) startScanCmd() tea.Cmd {
	m.scanCh = make(chan any, 256)
	m.scanning = true
	m.scanned = 0
	m.items = nil
	m.cursor = 0
	m.offset = 0
	m.startedAt = time.Now()
	go startScan(m.root, m.depth, m.includeMain, m.scanCh)
	return m.readScanCmd()
}

func (m *model) readScanCmd() tea.Cmd {
	if m.scanCh == nil {
		return nil
	}
	ch := m.scanCh
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return nil
		}
		return v
	}
}

type deleteStartMsg struct{ idx int }

func (m *model) deleteCmd(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.items) {
		return nil
	}
	it := m.items[idx]
	force := m.force
	return func() tea.Msg {
		ok, errMsg := removeWorktree(it.Repo, it.Path, force)
		return deleteResultMsg{idx: idx, ok: ok, err: errMsg}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Update
// ─────────────────────────────────────────────────────────────────────────────
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.spin++
		return m, tickCmd()

	case scanProgressMsg:
		m.scanned = msg.scanned
		m.current = msg.current
		return m, m.readScanCmd()

	case worktreeFoundMsg:
		m.items = append(m.items, msg.wt)
		sort.SliceStable(m.items, func(i, j int) bool {
			return m.items[i].Size > m.items[j].Size
		})
		return m, m.readScanCmd()

	case scanDoneMsg:
		m.scanning = false
		m.elapsed = msg.elapsed
		close(m.scanCh)
		m.scanCh = nil
		return m, nil

	case deleteResultMsg:
		if msg.idx < 0 || msg.idx >= len(m.items) {
			return m, nil
		}
		if msg.ok {
			m.items[msg.idx].Status = "deleted"
			m.flash(styOK.Render("✓ removed ") + styMuted.Render(m.items[msg.idx].Branch))
		} else {
			m.items[msg.idx].Status = "failed"
			m.items[msg.idx].StatusErr = msg.err
			m.flash(styErr.Render("✗ failed: ") + styMuted.Render(msg.err) + styDim.Render(" — press f to enable --force"))
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "pgup":
		m.cursor -= m.listH()
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "pgdown":
		m.cursor += m.listH()
		if m.cursor >= len(m.items) {
			m.cursor = len(m.items) - 1
		}
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.items) - 1
	case " ":
		if m.cursor >= 0 && m.cursor < len(m.items) {
			it := &m.items[m.cursor]
			if it.Status == "deleted" || it.Status == "deleting" {
				return m, nil
			}
			it.Status = "deleting"
			return m, m.deleteCmd(m.cursor)
		}
	case "f":
		m.force = !m.force
		m.flash(styMuted.Render("force = ") + boolBadge(m.force))
	case "m":
		m.includeMain = !m.includeMain
		m.flash(styMuted.Render("include main = ") + boolBadge(m.includeMain) + styDim.Render(" · rescanning…"))
		return m, m.startScanCmd()
	case "r":
		m.flash(styMuted.Render("rescanning…"))
		return m, m.startScanCmd()
	}
	return m, nil
}

func (m *model) flash(msg string) {
	m.flashMsg = msg
	m.flashEnd = time.Now().Add(2500 * time.Millisecond)
}

// ─────────────────────────────────────────────────────────────────────────────
// View
// ─────────────────────────────────────────────────────────────────────────────
const (
	padX = 3 // left + right padding (each)
	padY = 1 // top + bottom padding (each)
)

func (m *model) innerWidth() int { return m.width - padX*2 }

func (m *model) View() string {
	if m.innerWidth() < 60 {
		return styErr.Render("terminal too narrow — resize to at least 60 cols")
	}

	var b strings.Builder
	b.WriteString(strings.Repeat("\n", padY))
	b.WriteString(indent(m.renderHeader(), padX))
	b.WriteString("\n")
	b.WriteString(indent(m.renderTableHeader(), padX))
	b.WriteString("\n")
	b.WriteString(indent(m.renderList(), padX))
	b.WriteString(indent(m.renderFooter(), padX))
	b.WriteString(strings.Repeat("\n", padY))
	return b.String()
}

func indent(s string, n int) string {
	if n <= 0 {
		return s
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln != "" {
			lines[i] = pad + ln
		} else if i < len(lines)-1 {
			lines[i] = pad
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) listH() int {
	// header(2 lines: title+rule) + blank(1) + table header(1) + blank(1) + footer(3: rule+stats+help) + padY*2
	h := m.height - 8 - padY*2
	if h < 5 {
		h = 5
	}
	return h
}

func (m *model) renderHeader() string {
	w := m.innerWidth()
	logo := styLogo.Render("✦ wtkill")
	tag := styMuted.Render("git worktree cleaner ") + styDim.Render("by @ohernandezdev")
	left := logo + " " + tag

	rootDisplay := homeShorten(m.root)
	meta := strings.Join([]string{
		styMuted.Render("force ") + boolBadge(m.force),
		styMuted.Render("main ") + boolBadge(m.includeMain),
		styMuted.Render(rootDisplay),
	}, styDim.Render("  ·  "))

	pad := w - lg.Width(left) - lg.Width(meta)
	if pad < 1 {
		pad = 1
	}
	line := left + strings.Repeat(" ", pad) + meta
	rule := styDim.Render(strings.Repeat("─", w))
	return line + "\n" + rule + "\n"
}

type cols struct{ cursor, branch, repo, size, age, status int }

func (m *model) cols() cols {
	c := cols{cursor: 2, size: 9, age: 7, status: 16}
	gaps := 4
	remaining := m.innerWidth() - c.cursor - c.size - c.age - c.status - gaps
	if remaining < 30 {
		remaining = 30
	}
	c.branch = remaining * 45 / 100
	if c.branch < 18 {
		c.branch = 18
	}
	// cap branch column to keep things readable on huge terminals
	if c.branch > 50 {
		c.branch = 50
	}
	c.repo = remaining - c.branch
	if c.repo > 60 {
		c.repo = 60
	}
	return c
}

func (m *model) renderTableHeader() string {
	c := m.cols()
	h := styMuted.Render(strings.Repeat(" ", c.cursor) +
		padRight("BRANCH", c.branch) + " " +
		padRight("REPO", c.repo) + " " +
		padLeft("SIZE", c.size) + " " +
		padLeft("AGE", c.age) + " " +
		padRight("STATUS", c.status))
	return h
}

func (m *model) renderList() string {
	listH := m.listH()
	c := m.cols()

	// keep cursor in viewport
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+listH {
		m.offset = m.cursor - listH + 1
	}

	var b strings.Builder

	if len(m.items) == 0 {
		w := m.innerWidth()
		// centered empty/loading state
		for i := 0; i < listH; i++ {
			if i == listH/2-1 {
				var msg string
				if m.scanning {
					msg = styBrand.Render(spinChar(m.spin)) + "  " + styBold.Render("scanning…") + "  " + styMuted.Render(fmt.Sprintf("%d repos checked", m.scanned))
				} else {
					msg = styOK.Render("✓") + "  " + styBold.Render("no worktrees found") + "  " + styMuted.Render("your tree is clean")
				}
				b.WriteString(centerLine(msg, w))
			} else if i == listH/2+1 && m.scanning && m.current != "" {
				sub := styDim.Render(truncate(homeShorten(m.current), w-4))
				b.WriteString(centerLine(sub, w))
			} else {
				b.WriteString("\n")
			}
		}
		return b.String()
	}

	visible := m.items[m.offset:]
	if len(visible) > listH {
		visible = visible[:listH]
	}

	for i := 0; i < listH; i++ {
		if i >= len(visible) {
			b.WriteString("\n")
			continue
		}
		idx := m.offset + i
		it := visible[i]
		sel := idx == m.cursor

		branch := it.Branch
		if branch == "" {
			if it.Detached {
				branch = "(detached)"
			} else {
				branch = "?"
			}
		}
		repoName := lastTwoSegments(homeShorten(it.Repo))

		// status cell
		var status string
		switch {
		case it.Status == "deleted":
			status = styOK.Render("✓ deleted")
		case it.Status == "deleting":
			status = styWarn.Render(spinChar(m.spin) + " deleting")
		case it.Status == "failed":
			status = styErr.Render("✗ failed")
		case it.Missing:
			status = styErr.Render("● missing")
		case it.Prunable:
			status = styWarn.Render("● prunable")
		default:
			status = styDim.Render("—")
		}

		// age color
		ageStr := fmtAge(it.AgeDays)
		var ageRendered string
		switch {
		case it.AgeDays < 0:
			ageRendered = styDim.Render(ageStr)
		case it.AgeDays > 30:
			ageRendered = styErr.Render(ageStr)
		case it.AgeDays > 7:
			ageRendered = styWarn.Render(ageStr)
		default:
			ageRendered = styOK.Render(ageStr)
		}

		// size
		var sizeRendered string
		if it.Size > 0 {
			sizeRendered = styFG.Render(fmtSize(it.Size))
		} else {
			sizeRendered = styDim.Render("—")
		}

		// branch style — dimmed if deleted
		var branchRendered string
		if it.Status == "deleted" {
			branchRendered = styDim.Render(branch)
		} else {
			branchRendered = styBold.Render(branch)
		}

		cursorMark := "  "
		if sel {
			cursorMark = styAccent.Render("▶ ")
		}

		line := cursorMark +
			padRightStyled(branchRendered, branch, c.branch) + " " +
			padRightStyled(styMuted.Render(repoName), repoName, c.repo) + " " +
			padLeftStyled(sizeRendered, lg.Width(sizeRendered), c.size) + " " +
			padLeftStyled(ageRendered, lg.Width(ageRendered), c.age) + " " +
			status

		if sel {
			// fill line to width and apply selection bg
			vis := lg.Width(line)
			w := m.innerWidth()
			if vis < w {
				line += strings.Repeat(" ", w-vis)
			}
			line = stySel.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *model) renderFooter() string {
	w := m.innerWidth()
	rule := styDim.Render(strings.Repeat("─", w))

	var totalSize, freedSize int64
	pending := 0
	for _, it := range m.items {
		totalSize += it.Size
		if it.Status == "deleted" {
			freedSize += it.Size
		}
		if it.Status == "" {
			pending++
		}
	}

	var left string
	if m.scanning {
		left = styBrand.Render(spinChar(m.spin)) + " " +
			styMuted.Render("scanning ") +
			styBold.Render(fmt.Sprintf("%d", m.scanned)) +
			styMuted.Render(" repos · "+truncate(homeShorten(m.current), 50))
	} else {
		left = styOK.Render("✓ ") + styMuted.Render("scan done · ") +
			styBold.Render(fmt.Sprintf("%d", len(m.items))) +
			styMuted.Render(fmt.Sprintf(" worktrees · %.1fs", m.elapsed.Seconds()))
	}

	right := strings.Join([]string{
		styMuted.Render("total ") + styBold.Render(fmtSize(totalSize)),
		styMuted.Render("freed ") + styOK.Render(fmtSize(freedSize)),
		styMuted.Render("pending ") + styBold.Render(fmt.Sprintf("%d", pending)),
	}, "  ")

	pad := w - lg.Width(left) - lg.Width(right)
	if pad < 1 {
		pad = 1
	}
	stats := left + strings.Repeat(" ", pad) + right

	var help string
	switch {
	case m.cursor < len(m.items) && m.items[m.cursor].Status == "failed" && m.items[m.cursor].StatusErr != "":
		errMsg := truncate(m.items[m.cursor].StatusErr, w-30)
		help = styErr.Render("✗ ") + styFG.Render(errMsg) + styDim.Render("  · press ") + styFG.Render("f") + styDim.Render(" then ") + styFG.Render("space") + styDim.Render(" to retry with --force")
	case m.flashMsg != "" && time.Now().Before(m.flashEnd):
		help = m.flashMsg
	default:
		help = strings.Join([]string{
			keyHelp("↑↓", "navigate"),
			keyHelp("space", "delete"),
			keyHelp("f", "force"),
			keyHelp("m", "include main"),
			keyHelp("r", "rescan"),
			keyHelp("q", "quit"),
		}, styDim.Render(" · "))
	}

	return rule + "\n" + stats + "\n" + help
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────
func keyHelp(k, label string) string {
	return styFG.Render(k) + " " + styMuted.Render(label)
}

func boolBadge(v bool) string {
	if v {
		return styWarn.Render("ON")
	}
	return styDim.Render("off")
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinChar(i int) string {
	return spinFrames[(i%len(spinFrames)+len(spinFrames))%len(spinFrames)]
}

func fmtSize(b int64) string {
	if b <= 0 {
		return "—"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(b)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if f < 10 && i > 0 {
		return fmt.Sprintf("%.1f %s", f, units[i])
	}
	return fmt.Sprintf("%.0f %s", f, units[i])
}

func fmtAge(d int) string {
	if d < 0 {
		return "?"
	}
	if d == 0 {
		return "today"
	}
	if d == 1 {
		return "1d"
	}
	if d < 30 {
		return fmt.Sprintf("%dd", d)
	}
	if d < 365 {
		return fmt.Sprintf("%dmo", d/30)
	}
	return fmt.Sprintf("%dy", d/365)
}

func homeShorten(p string) string {
	usr, err := user.Current()
	if err != nil {
		return p
	}
	if strings.HasPrefix(p, usr.HomeDir) {
		return "~" + strings.TrimPrefix(p, usr.HomeDir)
	}
	return p
}

func lastTwoSegments(p string) string {
	parts := strings.Split(p, string(filepath.Separator))
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		if n <= 1 {
			return s[:n]
		}
		return s[:n-1] + "…"
	}
	return s + strings.Repeat(" ", n-len(s))
}
func padLeft(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return strings.Repeat(" ", n-len(s)) + s
}
func padRightStyled(rendered, raw string, n int) string {
	if len(raw) >= n {
		// truncate raw + restyle — but lipgloss styles are baked, so just truncate the rendered string approximately
		return truncateAnsi(rendered, n)
	}
	return rendered + strings.Repeat(" ", n-len(raw))
}
func padLeftStyled(rendered string, visualLen, n int) string {
	if visualLen >= n {
		return truncateAnsi(rendered, n)
	}
	return strings.Repeat(" ", n-visualLen) + rendered
}

// truncateAnsi truncates a styled string to n visible cells, appending …
func truncateAnsi(s string, n int) string {
	if lg.Width(s) <= n {
		return s
	}
	// fallback: use lipgloss to render to width
	return lg.NewStyle().MaxWidth(n).Render(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func centerLine(s string, w int) string {
	pad := (w - lg.Width(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s + "\n"
}
