package app

import (
	"fmt"
	"image"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
	"github.com/MiguelFPP/tui-port/internal/tui/textsafe"
)

// resize recomputes the canvas and the panel rectangles; the root owns hit-testing.
func (m *Model) resize() {
	width, height := m.canvas()
	body := bodyHeight(height)
	tableW := m.tableWidthNow(width)

	tableInner := innerRect(0, headerRows, tableW, body)
	m.table.SetBounds(tableInner.Min.X, tableInner.Min.Y, tableInner.Dx(), tableInner.Dy())
	m.tableRect = tableInner

	if m.inspectorCollapsed {
		// The hidden pane keeps the geometry of its visible form, so its viewport
		// does not collapse while it is out of sight.
		hiddenW := splitWidth(width, m.splitWidth)
		hidden := innerRect(hiddenW, headerRows, max(width-hiddenW, 0), body)
		m.detail.SetBounds(hidden.Min.X, hidden.Min.Y, hidden.Dx(), hidden.Dy())
		m.inspectorRect = image.Rectangle{}
	} else {
		inspectorInner := innerRect(tableW, headerRows, max(width-tableW, 0), body)
		m.detail.SetBounds(inspectorInner.Min.X, inspectorInner.Min.Y, inspectorInner.Dx(), inspectorInner.Dy())
		m.inspectorRect = inspectorInner
	}

	m.splitterRect = m.splitterBounds(tableW, body)
	m.collapseRect = m.collapseBounds(width)
	m.markTabs(width)
}

// splitterBounds is the two shared border cells of the table and the detail pane, or an empty rectangle.
func (m Model) splitterBounds(tableW, body int) image.Rectangle {
	if !m.canSplit() {
		return image.Rectangle{}
	}

	return image.Rect(tableW-1, headerRows, tableW+1, headerRows+body)
}

// collapseBounds is the right-aligned inspector button of the tab row, or empty when it does not fit.
func (m Model) collapseBounds(width int) image.Rectangle {
	size := lipgloss.Width(m.collapseLabel())
	if start := width - size; start >= m.tabLabelsWidth() {
		return image.Rect(start, headerRows-1, start+size, headerRows)
	}

	return image.Rectangle{}
}

// collapseLabel is the inspector button: it points right while the pane is shown and left while hidden.
func (m Model) collapseLabel() string {
	if m.inspectorCollapsed {
		return "[<]"
	}

	return "[>]"
}

// tabLabelsWidth is the rendered width of the tab labels and their separators.
func (m Model) tabLabelsWidth() int {
	total := 0
	for tab := Tab(0); tab < tabCount; tab++ {
		total += lipgloss.Width(tabLabels[tab])
		if tab > 0 {
			total += lipgloss.Width(tabSeparator)
		}
	}

	return total
}

// innerRect is the area left inside the frame of a box at x, y with the given outer size.
func innerRect(x, y, width, height int) image.Rectangle {
	x, y = x+borderSide, y+borderSide

	return image.Rect(x, y, x+innerCells(width), y+innerCells(height))
}

// innerCells returns the cells left by the frame of a box, never negative.
func innerCells(size int) int {
	return max(size-2*borderSide, 0)
}

// markTabs stores the rectangle of every top tab, on the row between the title and the body.
func (m *Model) markTabs(width int) {
	m.tabRects = [tabCount]image.Rectangle{}

	column := 0
	for tab := Tab(0); tab < tabCount; tab++ {
		size := lipgloss.Width(tabLabels[tab])
		if column+size > width {
			return // the labels a narrow row cannot show are not clickable
		}

		m.tabRects[tab] = image.Rect(column, headerRows-1, column+size, headerRows)
		column += size + lipgloss.Width(tabSeparator)
	}
}

func (m Model) canvas() (int, int) {
	return max(m.width, minWidth), max(m.height, minHeight)
}

func bodyHeight(height int) int {
	return max(height-headerRows-footerRows, 0)
}

// splitWidth reserves the left column for the table: the explicit width when given,
// a percentage otherwise, always capped so the detail pane keeps a minimum.
func splitWidth(total, explicit int) int {
	reserve := explicit
	if reserve == 0 {
		reserve = total * tablePercent / 100
	}

	table := clamp(reserve, minTable, max(total-minInspector, minTable))

	return clamp(table, 0, total)
}

// render composes the header, the active tab and the statusbar.
func (m Model) render() string {
	width, height := m.canvas()

	lines := make([]string, 0, height)
	lines = append(lines, styles.Title.Render(clipCells(" tui-port ", width)))
	lines = append(lines, m.tabsLine(width))
	lines = append(lines, m.body(width, height)...)
	lines = append(lines, m.statusLine(width))

	content := strings.Join(lines, "\n")
	switch {
	case m.help:
		return m.helpOverlay(content, width, height)
	case m.configOpen:
		return m.configOverlay(content, width, height)
	}

	return content
}

// body renders the framed table and detail panes, always exactly bodyHeight rows high.
func (m Model) body(width, height int) []string {
	rows := bodyHeight(height)
	tableW := m.tableWidthNow(width)
	inspectorW := max(width-tableW, 0)

	tableLines := fitBox(m.box(m.table.View(), tableW, rows, m.focus == FocusTable), tableW, rows)
	if m.inspectorCollapsed {
		return tableLines // a hidden pane leaves the whole width to the table
	}

	inspectorLines := fitBox(m.box(m.detail.View(), inspectorW, rows, m.focus == FocusInspector), inspectorW, rows)

	out := make([]string, 0, rows)
	for i := range tableLines {
		out = append(out, tableLines[i]+inspectorLines[i])
	}

	if m.dragging {
		out = m.paintSplitter(out, tableW)
	}

	return out
}

// box draws content inside a framed box of exactly width x height cells; focused picks the token.
func (m Model) box(content string, width, height int, focused bool) string {
	innerWidth, innerHeight := innerCells(width), innerCells(height)
	if innerWidth < 1 || innerHeight < 1 {
		// No room for a frame: the bare content keeps the layout tiling exactly.
		return strings.Join(fitBox(content, width, height), "\n")
	}

	lines := fitBox(content, innerWidth, innerHeight)
	for i, line := range lines {
		lines[i] = line + resetStyle // an attribute left open must not reach the frame
	}

	style := styles.Border
	if focused {
		style = styles.BorderFocused
	}

	return style.Render(strings.Join(lines, "\n"))
}

// tabsLine renders the top labels plus the inspector button on the right.
func (m Model) tabsLine(width int) string {
	row := m.tabLabelsLine()
	if rect := m.collapseBounds(width); rect.Dx() > 0 {
		row = padLine(row, rect.Min.X) + styles.Tab.Render(m.collapseLabel())
	}

	return padLine(row, width)
}

// tabLabelsLine renders the labels of every tab with their separators.
func (m Model) tabLabelsLine() string {
	parts := make([]string, 0, tabCount*2-1)

	for tab := Tab(0); tab < tabCount; tab++ {
		if tab > 0 {
			parts = append(parts, styles.TabSeparator.Render(tabSeparator))
		}

		style := styles.Tab
		if tab == m.tab {
			style = styles.TabActive
		}

		parts = append(parts, style.Render(tabLabels[tab]))
	}

	return strings.Join(parts, "")
}

// statusLine shows the totals, the tab, the focus, the hints, the spinner and the toast.
func (m Model) statusLine(width int) string {
	counts := m.table.Counts()

	parts := []string{
		fmt.Sprintf("puertos: %d", counts.Ports),
		fmt.Sprintf("listen: %d", counts.Listening),
		fmt.Sprintf("conectadas: %d", counts.Connected),
		fmt.Sprintf("procs: %d", counts.Processes),
		"rss: " + humanBytes(counts.RSS),
		"tab: " + m.tab.String(),
		"foco: " + m.focusLabel(),
		m.tabsHint(),
		m.keys.Label(keymap.ActionFocusNext) + " foco",
		m.keys.Label(keymap.ActionHelp) + " ayuda",
		m.keys.Label(keymap.ActionQuit) + " salir",
	}

	text := strings.Join(parts, " · ")
	if m.toast != "" {
		text = m.toast + " · " + text
	}
	if m.loading() {
		text = m.spinner.View() + " " + text
	}

	return styles.Status.Render(clipCells(text, width))
}

// tabsHint names the keys that switch the top tabs, in tab order.
func (m Model) tabsHint() string {
	actions := [...]keymap.Action{keymap.ActionTabListen, keymap.ActionTabConnected, keymap.ActionTabAll}

	labels := make([]string, 0, len(actions))
	for _, action := range actions {
		labels = append(labels, m.keys.Label(action))
	}

	return strings.Join(labels, "/") + " tabs"
}

// helpOverlay centers the help box over the layout without changing its size.
func (m Model) helpOverlay(base string, width, height int) string {
	lines := helpLines(m.keys)

	// A long line must not widen the box past the canvas and break the layout.
	limit := max(width-overlayChrome, 1)
	for index, line := range lines {
		lines[index] = clipCells(line, limit)
	}

	box := styles.Help.Render(strings.Join(lines, "\n"))

	return overlayBox(base, box, width, height)
}

// configOverlay centers the read-only configuration box over the layout.
func (m Model) configOverlay(base string, width, height int) string {
	box := styles.Help.Render(strings.Join(m.configLines(width), "\n"))

	return overlayBox(base, box, width, height)
}

// configLines renders the configuration summary: the file, the theme, the key
// overrides and the restart notice. Long lines are clipped to the canvas.
func (m Model) configLines(width int) []string {
	actions := m.overrideActions()

	lines := make([]string, 0, len(actions)+8)
	lines = append(lines, styles.HelpTitle.Render("configuración"))
	lines = append(lines, "archivo: "+m.configFileLabel())
	lines = append(lines, "tema: "+styles.CurrentTheme().Name)
	lines = append(lines, fmt.Sprintf("overrides: %d", len(actions)))
	lines = append(lines, "ancho de la tabla: "+m.splitLabel())

	if len(actions) == 0 {
		lines = append(lines, "sin overrides")
	} else {
		for _, action := range actions {
			keys := textsafe.Sanitize(strings.Join(m.cfg.Keys[action], " / "))
			lines = append(lines, "  "+textsafe.Sanitize(action)+": "+keys)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "edita el archivo y reinicia para aplicar")
	lines = append(lines, m.keys.Label(keymap.ActionConfig)+" / esc: cerrar")

	// A long path must not widen the box past the canvas and break the layout.
	limit := max(width-overlayChrome, 1)
	for index, line := range lines {
		lines[index] = clipCells(line, limit)
	}

	return lines
}

// splitLabel names the persisted table width and the inspector state.
func (m Model) splitLabel() string {
	width := "auto"
	if m.splitWidth > 0 {
		width = fmt.Sprintf("%d", m.splitWidth)
	}
	if m.inspectorCollapsed {
		width += " · detalle minimizado"
	}

	return width
}

// configFileLabel names the loaded file, or reports the built-in defaults.
func (m Model) configFileLabel() string {
	if strings.TrimSpace(m.cfg.ConfigPath) == "" {
		return "sin archivo (defaults)"
	}

	return textsafe.Sanitize(m.cfg.ConfigPath)
}

// overrideActions returns the actions with at least one override key, sorted.
func (m Model) overrideActions() []string {
	actions := make([]string, 0, len(m.cfg.Keys))
	for action, keys := range m.cfg.Keys {
		if len(keys) > 0 {
			actions = append(actions, action)
		}
	}

	sort.Strings(actions)

	return actions
}

// overlayBox centers an already rendered box over base without resizing base.
func overlayBox(base, box string, width, height int) string {
	boxLines := strings.Split(box, "\n")

	left := max((width-lipgloss.Width(box))/2, 0)
	top := max((height-len(boxLines))/2, 0)

	lines := strings.Split(base, "\n")
	for i, line := range boxLines {
		row := top + i
		if row < 0 || row >= len(lines) {
			continue
		}

		lines[row] = strings.Repeat(" ", left) + line
	}

	return strings.Join(lines, "\n")
}

// splitterGlyph is the colour of the shared border: the accent marks the handle while dragging.
func (m Model) splitterGlyph() lipgloss.Style {
	if m.dragging {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(styles.CurrentTheme().Accent))
	}

	return lipgloss.NewStyle().Foreground(lipgloss.Color(styles.CurrentTheme().Border))
}

// paintSplitter redraws the two cells of the shared border with the handle token.
func (m Model) paintSplitter(rows []string, tableW int) []string {
	if !m.canSplit() {
		return rows
	}

	handle := m.splitterGlyph().Render("││")

	// The top and the bottom rows keep the corners of the two boxes.
	for i := 1; i < len(rows)-1; i++ {
		rows[i] = clipCells(rows[i], tableW-1) + handle + tailCells(rows[i], tableW+1)
	}

	return rows
}

// fitBox splits a rendered view into exactly height rows, each padded to width cells.
func fitBox(view string, width, height int) []string {
	lines := strings.Split(view, "\n")
	out := make([]string, 0, max(height, 0))

	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}

		out = append(out, padLine(line, width))
	}

	return out
}

// padLine clips line to width cells and fills the rest with spaces.
func padLine(line string, width int) string {
	if width <= 0 {
		return ""
	}

	line = clipCells(line, width)
	if gap := width - lipgloss.Width(line); gap > 0 {
		return line + strings.Repeat(" ", gap)
	}

	return line
}

// clipCells cuts a styled string to width visible cells, keeping the ANSI intact.
func clipCells(s string, width int) string {
	if width <= 0 {
		return ""
	}

	var b strings.Builder
	visible := 0
	inEscape := false

	for _, r := range s {
		if r == 0x1b {
			inEscape = true
			b.WriteRune(r)

			continue
		}
		if inEscape {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}

			continue
		}
		if visible >= width {
			break
		}

		b.WriteRune(r)
		visible++
	}

	return b.String()
}

// tailCells returns an ANSI line from its n-th visible cell, keeping the escapes of that cell.
func tailCells(s string, cells int) string {
	if cells <= 0 {
		return s
	}

	var pending strings.Builder
	visible := 0
	inEscape := false

	for i, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
			pending.WriteRune(r)
		case inEscape:
			pending.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		case visible == cells:
			return pending.String() + s[i:]
		default:
			visible++
			pending.Reset()
		}
	}

	return ""
}

// humanBytes renders a byte count with the largest unit that fits it.
func humanBytes(size uint64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)

	switch {
	case size >= gib:
		return fmt.Sprintf("%.1f GiB", float64(size)/float64(gib))
	case size >= mib:
		return fmt.Sprintf("%.1f MiB", float64(size)/float64(mib))
	case size >= kib:
		return fmt.Sprintf("%.1f KiB", float64(size)/float64(kib))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

func clamp(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}

	return value
}
