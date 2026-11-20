package app

import (
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
)

// helpLines renders the full keymap shown by the help overlay: the root scope of
// the registry, plus the keys every panel documents in its own footer.
func helpLines(keys keymap.Registry) []string {
	bindings := keys.Bindings(keymap.ScopeRoot)

	lines := make([]string, 0, len(bindings)+10)
	lines = append(lines, styles.HelpTitle.Render("tui-port · atajos"))
	lines = append(lines, "")

	for _, binding := range bindings {
		lines = append(lines, styles.HelpKey.Render(keys.Label(binding.Action))+"  "+binding.Desc)
	}

	lines = append(lines, styles.HelpKey.Render("esc")+"  cerrar la ayuda")
	lines = append(lines, "")
	lines = append(lines, "puertos: "+keys.Label(keymap.ActionPortsSort)+" cambia el orden, "+
		keys.Label(keymap.ActionPortsFilter)+" filtra, "+keys.Label(keymap.ActionPortsKill)+" mata con SIGKILL y "+
		keys.Label(keymap.ActionPortsTerm)+" con SIGTERM")
	lines = append(lines, "detalle: "+keys.Label(keymap.ActionFocusNext)+" cambia el foco y "+
		keys.Label(keymap.ActionInspectorUp)+"/"+keys.Label(keymap.ActionInspectorDown)+" lo desplazan")
	lines = append(lines, "layout: "+keys.Label(keymap.ActionToggleInspector)+" minimiza o muestra el detalle, "+
		keys.Label(keymap.ActionSplitShrink)+"/"+keys.Label(keymap.ActionSplitGrow)+" redimensionan la tabla")

	return lines
}
