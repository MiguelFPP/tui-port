package inspector

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
	"github.com/MiguelFPP/tui-port/internal/tui/textsafe"
)

const (
	// titleText names the pane; the root draws the frame around it.
	titleText = "detalle · socket"

	// placeholderText is the empty state of the pane.
	placeholderText = "selecciona un puerto"

	// maxCmdlineLines bounds the wrapped command line before it is cut.
	maxCmdlineLines = 6

	// startedLayout is the timestamp format of the process start.
	startedLayout = "2006-01-02 15:04:05"

	// columnSeparator joins the fields of the footer.
	columnSeparator = " · "

	// indent prefixes every row of a section, marker flags the selected socket
	// and cmdlineLabel introduces the wrapped command line.
	indent       = "  "
	marker       = "▸"
	cmdlineLabel = "  cmdline: "
)

// View renders exactly height rows: the title, the scrollable detail and the
// hints of the pane.
func (m Model) View() string {
	if m.height <= 0 {
		return ""
	}

	lines := make([]string, 0, m.height)
	lines = append(lines, m.titleLine())
	lines = append(lines, m.bodyLines()...)
	lines = append(lines, m.footerLine())

	lines = fitLines(lines, m.height)
	for index := range lines {
		lines[index] = padLine(lines[index], m.width)
	}

	return strings.Join(lines, "\n")
}

// titleLine names the pane; a narrow pane clips it instead of overflowing.
func (m Model) titleLine() string {
	return styles.TitlePlain.Render(clipCells(titleText, m.width))
}

// bodyLines is the viewport area: the rendered content or the empty-state placeholder.
func (m Model) bodyLines() []string {
	height := m.bodyHeight()
	if !m.ok {
		return m.placeholderLines(height)
	}

	return fitLines(strings.Split(m.viewport.View(), "\n"), height)
}

// placeholderLines centres the empty-state text inside the viewport area.
func (m Model) placeholderLines(height int) []string {
	lines := make([]string, max(height, 0))

	text := clipCells(placeholderText, m.width)
	if len(lines) == 0 || text == "" {
		return lines
	}

	left := max((m.width-cellWidth(text))/2, 0)
	lines[len(lines)/2] = strings.Repeat(" ", left) + styles.Placeholder.Render(text)

	return lines
}

// footerLine lists the keys that scroll the pane and how far it is scrolled.
func (m Model) footerLine() string {
	return styles.MetaPlain.Render(clipCells(m.footerText(), m.width))
}

// footerText composes the hints of the inspector scope and the scroll position.
func (m Model) footerText() string {
	parts := []string{
		m.keys.Label(keymap.ActionInspectorUp) + ", " + m.keys.Label(keymap.ActionInspectorDown) + ": desplazar",
		m.keys.Label(keymap.ActionInspectorPageUp) + "/" + m.keys.Label(keymap.ActionInspectorPageDown) + ": página",
	}

	// A pane without a selection has nothing to scroll, so the percentage would lie.
	if m.ok {
		parts = append(parts, fmt.Sprintf("%.0f%%", m.viewport.ScrollPercent()*100))
	}

	return strings.Join(parts, columnSeparator)
}

// contentLines renders the scrollable body: the socket, the owning process and
// the ports of that process.
func (m Model) contentLines() []string {
	if m.width <= 0 {
		return nil
	}

	width := m.width
	lines := make([]string, 0, 24)
	lines = append(lines, sectionLine("socket", width))
	lines = append(lines, m.socketLines(width)...)
	lines = append(lines, "")
	lines = append(lines, sectionLine("proceso", width))

	if m.attributed() {
		lines = append(lines, m.processLines(width)...)
		lines = append(lines, "")
		lines = append(lines, m.portLines(sortedSiblings(m.siblings), width)...)
	} else {
		lines = append(lines, m.unattributedLines(width)...)
	}

	return trimBlank(lines)
}

// sectionLine renders one section header.
func sectionLine(title string, width int) string {
	return styles.TitlePlain.Render(clipCells(title, width))
}

// socketLines renders the fields of the transport endpoint.
func (m Model) socketLines(width int) []string {
	return []string{
		clipCells(indent+"proto: "+m.socket.Protocol.String(), width),
		clipCells(indent+"local: "+endpointLabel(m.socket.Local), width),
		clipCells(indent+"remoto: "+remoteLabel(m.socket), width),
		clipCells(indent+"estado: "+m.socket.State.String(), width),
		clipCells(fmt.Sprintf("%suid: %d · inode: %d", indent, m.socket.UID, m.socket.Inode), width),
	}
}

// processLines renders the owner of the socket and its command line.
func (m Model) processLines(width int) []string {
	lines := []string{
		clipCells(indent+"nombre: "+processLabel(m.process), width),
		clipCells(fmt.Sprintf("%susuario: %s · ppid: %d", indent, valueOrDash(m.process.User), m.process.PPID), width),
		clipCells(fmt.Sprintf("%srss: %s · hilos: %d", indent, humanBytes(m.process.RSS), m.process.Threads), width),
		clipCells(indent+"inicio: "+startedLabel(m.process.StartedAt, time.Now()), width),
	}

	return append(lines, cmdlineLines(m.process.Cmdline, width)...)
}

// unattributedLines explains why a socket has no owner and how to resolve it.
func (m Model) unattributedLines(width int) []string {
	return []string{
		clipCells(indent+"sin proceso identificable", width),
		clipCells(fmt.Sprintf("%suid %d · ejecuta como root para ver todos los procesos", indent, m.socket.UID), width),
	}
}

// portLines lists every port of the process, with the selected socket marked.
func (m Model) portLines(siblings []domain.Socket, width int) []string {
	lines := make([]string, 0, len(siblings)+1)
	lines = append(lines, sectionLine(fmt.Sprintf("puertos del proceso (%d)", len(siblings)), width))

	if len(siblings) == 0 {
		return append(lines, clipCells(indent+"sin puertos", width))
	}

	selected := m.socket.ID()
	for _, sibling := range siblings {
		glyph := " "
		if sibling.ID() == selected {
			glyph = marker
		}

		row := indent + glyph + " " + sibling.Protocol.String() + " " + sibling.Local.String() + " " + sibling.State.String()
		lines = append(lines, clipCells(row, width))
	}

	return lines
}

// processLabel names the process and appends its pid when the name is known.
// The name is chosen by the process itself, so it is neutralized first.
func processLabel(process domain.Process) string {
	name := strings.TrimSpace(textsafe.Sanitize(process.Name))
	if name == "" {
		return "pid " + strconv.Itoa(process.PID)
	}

	return name + " (pid " + strconv.Itoa(process.PID) + ")"
}

// endpointLabel renders an endpoint, or a dash when the kernel reported none.
func endpointLabel(endpoint domain.Endpoint) string {
	if endpoint.Addr == "" && endpoint.Port == 0 {
		return "-"
	}

	return endpoint.String()
}

// remoteLabel renders the peer of the socket, or a dash when it has none.
func remoteLabel(socket domain.Socket) string {
	if !socket.HasPeer() {
		return "-"
	}

	return socket.Remote.String()
}

// valueOrDash keeps a row readable when the snapshot could not resolve the
// field; the value is neutralized because it also comes from the process.
func valueOrDash(value string) string {
	value = strings.TrimSpace(textsafe.Sanitize(value))
	if value == "" {
		return "-"
	}

	return value
}

// humanBytes renders a byte count with the largest unit that fits it, with one
// decimal from KiB up.
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
		return strconv.FormatUint(size, 10) + " B"
	}
}

// startedLabel renders the start time and how long the process has been running.
func startedLabel(started, now time.Time) string {
	if started.IsZero() {
		return "-"
	}

	return started.Format(startedLayout) + " (" + uptimeLabel(started, now) + ")"
}

// uptimeLabel summarises a duration as "Nd Nh", "Nh Nm" or "Nm".
func uptimeLabel(started, now time.Time) string {
	elapsed := now.Sub(started)
	if elapsed < 0 {
		elapsed = 0 // a clock skew must not render a negative age
	}

	days := int(elapsed / (24 * time.Hour))
	hours := int(elapsed/time.Hour) % 24
	minutes := int(elapsed/time.Minute) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// sanitizeCmdline keeps the command line printable: tabs become four spaces and
// every control or format rune becomes a space.
func sanitizeCmdline(cmdline string) string {
	return strings.TrimSpace(textsafe.Sanitize(cmdline))
}

// cmdlineLines wraps the command line under its label, keeps every row within
// width and marks the overflow with an ellipsis.
func cmdlineLines(cmdline string, width int) []string {
	if width <= 0 {
		return nil
	}

	text := sanitizeCmdline(cmdline)
	if text == "" {
		return []string{clipCells(cmdlineLabel+"-", width)}
	}

	pad := strings.Repeat(" ", len(cmdlineLabel))
	chunks := wrapWords(text, max(width-len(cmdlineLabel), 1))

	total := min(len(chunks), maxCmdlineLines)
	lines := make([]string, 0, total)
	for index := 0; index < total; index++ {
		prefix := pad
		if index == 0 {
			prefix = cmdlineLabel
		}

		lines = append(lines, clipCells(prefix+chunks[index], width))
	}

	if len(chunks) > total {
		last := len(lines) - 1
		if cellWidth(lines[last]) < width {
			lines[last] += "…"
		} else {
			lines[last] = markCut(lines[last], width)
		}
	}

	return lines
}

// wrapWords packs the words of text into rows of at most limit cells, cutting
// any word that does not fit on a row of its own.
func wrapWords(text string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	rows := make([]string, 0, 4)
	var line strings.Builder
	used := 0

	flush := func() {
		rows = append(rows, line.String())
		line.Reset()
		used = 0
	}

	for _, word := range strings.Fields(text) {
		for cellWidth(word) > limit {
			if used > 0 {
				flush()
			}

			head, tail := splitCells(word, limit)
			rows = append(rows, head)
			word = tail
		}

		if word == "" {
			continue
		}

		gap := 0
		if used > 0 {
			gap = 1
		}
		if used+gap+cellWidth(word) > limit {
			flush()
			gap = 0
		}

		if gap > 0 {
			line.WriteByte(' ')
		}

		line.WriteString(word)
		used += gap + cellWidth(word)
	}

	if used > 0 || len(rows) == 0 {
		flush()
	}

	return rows
}

// splitCells cuts text after the runes that fill limit cells. The head is never
// empty, so a wide rune on a narrow row still makes progress.
func splitCells(text string, limit int) (string, string) {
	width := 0

	for index, r := range text {
		size := cellWidth(string(r))
		if width > 0 && width+size > limit {
			return text[:index], text[index:]
		}

		width += size
	}

	return text, ""
}

// markCut replaces the tail of a full row with an ellipsis.
func markCut(line string, width int) string {
	switch {
	case width <= 0:
		return ""
	case width == 1:
		return "…"
	default:
		return clipCells(line, width-1) + "…"
	}
}

// trimBlank drops the trailing blank rows that would only add scroll range.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// fitLines clips or pads lines to exactly height rows.
func fitLines(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) > height {
		return lines[:height]
	}

	for len(lines) < height {
		lines = append(lines, "")
	}

	return lines
}

// clipCells cuts unrendered text to width cells; a line that already carries
// escapes goes through padLine instead.
func clipCells(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}
	if cellWidth(text) <= width {
		return text
	}

	var b strings.Builder
	b.Grow(len(text))

	used := 0
	for _, r := range text {
		size := cellWidth(string(r))
		if used+size > width {
			break
		}

		b.WriteRune(r)
		used += size
	}

	return b.String()
}

// padLine clips a rendered line to width cells and fills the rest with spaces.
func padLine(line string, width int) string {
	if width <= 0 {
		return ""
	}

	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}

// cellWidth is the number of terminal cells a fragment occupies.
func cellWidth(text string) int {
	return lipgloss.Width(text)
}
