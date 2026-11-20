package porttable

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/MiguelFPP/tui-port/internal/domain"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
)

// View composes the title, the column header, the filter line when it is
// active, the visible rows and the footer. It returns exactly height lines and
// every line is clipped or padded to width, so the root can frame it.
func (m Model) View() string {
	if m.height <= 0 {
		return ""
	}

	lines := make([]string, 0, m.height)
	lines = append(lines, styles.Title.Render(m.cell(m.titleText())))
	lines = append(lines, styles.Header.Render(m.cell(m.header)))

	if m.filterRows() > 0 {
		lines = append(lines, m.filterLine())
	}

	if line, ok := m.stateLine(); ok {
		lines = append(lines, line)
	} else {
		lines = append(lines, m.bodyRows()...)
	}

	lines = append(lines, styles.Meta.Render(m.cell(m.footerText())))

	return strings.Join(m.fit(lines), "\n")
}

// cell clips and pads plain text to the width of the panel.
func (m Model) cell(text string) string {
	return padRight(truncate(text, m.width), m.width)
}

// fit trims or pads the composed lines to the exact height of the panel.
func (m Model) fit(lines []string) []string {
	if len(lines) > m.height {
		return lines[:m.height]
	}

	blank := m.cell("")
	for len(lines) < m.height {
		lines = append(lines, blank)
	}

	return lines
}

// titleText is the header: the tab, how many rows it shows and the order.
func (m Model) titleText() string {
	return strings.Join([]string{
		scopeLabel(m.scope),
		countLabel(len(m.rows)),
		"orden: " + m.order.String(),
	}, columnSeparator)
}

// scopeLabel names the tab of the table.
func scopeLabel(scope domain.Scope) string {
	switch scope {
	case domain.ScopeListen:
		return "escuchando"
	case domain.ScopeConnected:
		return "conectadas"
	default:
		return "todas"
	}
}

// countLabel names how many rows the table shows.
func countLabel(count int) string {
	switch count {
	case 0:
		return "sin puertos"
	case 1:
		return "1 puerto"
	default:
		return strconv.Itoa(count) + " puertos"
	}
}

// stateLine is the message that replaces the rows while there is nothing to show.
func (m Model) stateLine() (string, bool) {
	switch {
	case len(m.rows) > 0:
		return "", false
	case m.err == nil && !m.loaded:
		return styles.Meta.Render(m.cell("cargando…")), true
	case errors.Is(m.err, domain.ErrUnavailable), errors.Is(m.err, domain.ErrUnsupported):
		return styles.Fail.Render(m.cell(m.unavailableText())), true
	case m.err != nil:
		return styles.Fail.Render(m.cell("error: " + describeError(m.err))), true
	case m.needle != "":
		return styles.Meta.Render(m.cell("sin coincidencias para el filtro")), true
	default:
		return styles.Meta.Render(m.cell("sin puertos en esta pestaña")), true
	}
}

// unavailableText explains an unusable monitor and names the key that retries.
func (m Model) unavailableText() string {
	hint := " · pulsa " + m.keys.Label(keymap.ActionPortsRefresh) + " para reintentar"

	if errors.Is(m.err, domain.ErrUnsupported) {
		return "sistema no soportado" + hint
	}

	return "monitor no disponible · revisa el acceso a los procesos del sistema" + hint
}

// bodyRows renders the visible window of the table, padded to the body height.
func (m Model) bodyRows() []string {
	height := m.bodyHeight()
	lines := make([]string, 0, height)

	end := min(m.offset+height, len(m.rows))
	for index := m.offset; index < end; index++ {
		line := m.cell(m.rows[index].text)
		if index == m.cursor {
			line = styles.CursorPlain.Render(line)
		}

		lines = append(lines, line)
	}

	blank := m.cell("")
	for len(lines) < height {
		lines = append(lines, blank)
	}

	return lines
}

// filterLine renders the input while it is being edited and the active filter
// otherwise; it is the line that cuts one row from the body.
func (m Model) filterLine() string {
	if !m.editing {
		return styles.Meta.Render(m.cell("filtro: " + m.filter))
	}

	line := m.input.View()
	if lipgloss.Width(line) > m.width {
		return styles.Meta.Render(m.cell(filterPrompt + m.filter))
	}

	return line + strings.Repeat(" ", max(m.width-lipgloss.Width(line), 0))
}

// footerText lists the notice of the last action, the active failure and the
// hints of the panel.
func (m Model) footerText() string {
	parts := make([]string, 0, 9)

	switch {
	case m.writing:
		parts = append(parts, "enviando señal…")
	case m.notice != "":
		parts = append(parts, m.notice)
	}

	switch {
	case m.actionErr != nil:
		parts = append(parts, "error: "+describeError(m.actionErr))
	case m.err != nil && len(m.rows) > 0:
		parts = append(parts, "error: "+describeError(m.err))
	}

	if m.editing {
		parts = append(parts, m.keys.Label(keymap.ActionFilterApply)+"/"+
			m.keys.Label(keymap.ActionFilterCancel)+": cerrar el filtro")
	}

	parts = append(parts,
		m.keys.Label(keymap.ActionPortsUp)+"/"+m.keys.Label(keymap.ActionPortsDown)+": mover",
		m.keys.Label(keymap.ActionPortsSort)+": orden",
		m.keys.Label(keymap.ActionPortsFilter)+" filtro",
		m.keys.Label(keymap.ActionPortsKill)+" kill -9",
		m.keys.Label(keymap.ActionPortsTerm)+" SIGTERM",
		m.keys.Label(keymap.ActionPortsRefresh)+" refrescar",
	)

	return strings.Join(parts, columnSeparator)
}
