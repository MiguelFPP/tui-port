// Package keymap centralizes the keyboard catalog of the TUI: the default
// bindings per scope, the canonical key parser, the validation of user
// overrides, the conflicts inside a scope and the reserved keys.
//
// Production code only depends on the standard library: the parser understands
// the canonical strings produced by Bubble Tea's KeyPressMsg.
package keymap

import (
	"fmt"
	"sort"
	"strings"
)

// Scope identifies a keyboard context: a screen, a panel or a modal.
type Scope string

// Scopes of the application.
const (
	ScopeRoot      Scope = "root"
	ScopePorts     Scope = "ports"
	ScopeInspector Scope = "inspector"
	ScopeFilter    Scope = "filter"
)

// Action is the stable identifier of one user-facing command.
type Action string

// Actions of the root scope.
const (
	ActionQuit            Action = "quit"
	ActionHelp            Action = "help"
	ActionConfig          Action = "config"
	ActionTabListen       Action = "tab_listen"
	ActionTabConnected    Action = "tab_connected"
	ActionTabAll          Action = "tab_all"
	ActionFocusNext       Action = "focus_next"
	ActionFocusPrev       Action = "focus_prev"
	ActionToggleInspector Action = "toggle_inspector"
	ActionSplitShrink     Action = "split_shrink"
	ActionSplitGrow       Action = "split_grow"
)

// Actions of the ports table scope.
const (
	ActionPortsUp       Action = "ports_up"
	ActionPortsDown     Action = "ports_down"
	ActionPortsPageUp   Action = "ports_page_up"
	ActionPortsPageDown Action = "ports_page_down"
	ActionPortsHome     Action = "ports_home"
	ActionPortsEnd      Action = "ports_end"
	ActionPortsSort     Action = "ports_sort"
	ActionPortsFilter   Action = "ports_filter"
	ActionPortsClear    Action = "ports_clear_filter"
	ActionPortsKill     Action = "ports_kill"
	ActionPortsTerm     Action = "ports_term"
	ActionPortsRefresh  Action = "ports_refresh"
)

// Actions of the inspector scope.
const (
	ActionInspectorUp       Action = "inspector_up"
	ActionInspectorDown     Action = "inspector_down"
	ActionInspectorPageUp   Action = "inspector_page_up"
	ActionInspectorPageDown Action = "inspector_page_down"
	ActionInspectorHome     Action = "inspector_home"
	ActionInspectorEnd      Action = "inspector_end"
)

// Actions of the filter input scope.
const (
	ActionFilterApply  Action = "filter_apply"
	ActionFilterCancel Action = "filter_cancel"
)

// Binding is one action inside a scope together with its keys and description.
//
// Keys are the configurable keys; FixedKeys are always active and cannot be
// overridden. Configurable reports whether the user may replace Keys.
type Binding struct {
	Action       Action
	Scope        Scope
	Keys         []string
	FixedKeys    []string
	Desc         string
	Configurable bool
}

// Registry is an immutable catalog indexed by action and by scope. The zero
// value is not usable: build it with Default or Apply.
type Registry struct {
	bindings []Binding
	actions  map[Action]int
	scopes   map[Scope]map[string]Action
}

// Default returns the built-in registry. It is the only source of defaults and
// descriptions, and it never shares slices with the returned value.
func Default() Registry {
	return mustRegistry(cloneBindings(catalog))
}

// Apply returns a new registry with overrides applied on top of r. It never
// mutates r nor the defaults, so registries can be chained. Overrides are
// validated in action order to keep the reported error deterministic.
func (r Registry) Apply(overrides map[string][]string) (Registry, error) {
	bindings := cloneBindings(r.bindings)

	for _, name := range sortedActions(overrides) {
		action := Action(name)

		index, ok := r.actions[action]
		if !ok {
			return Registry{}, fmt.Errorf("keymap: acción desconocida %q", name)
		}

		binding := &bindings[index]
		if !binding.Configurable {
			return Registry{}, fmt.Errorf("keymap: la acción %q no es configurable", name)
		}

		keys, err := normalizeKeys(action, overrides[name])
		if err != nil {
			return Registry{}, err
		}

		binding.Keys = keys
	}

	return newRegistry(bindings)
}

// Match canonicalizes key and resolves it inside scope.
func (r Registry) Match(scope Scope, key string) (Action, bool) {
	canonical := CanonicalKey(key)
	if canonical == "" {
		return "", false
	}

	keys, ok := r.scopes[scope]
	if !ok {
		return "", false
	}

	action, ok := keys[canonical]

	return action, ok
}

// Keys returns the effective keys of action (configurable plus fixed) in order.
// The result is always a fresh slice: callers never see the internal one.
func (r Registry) Keys(action Action) []string {
	index, ok := r.actions[action]
	if !ok {
		return nil
	}

	return effectiveKeys(r.bindings[index])
}

// Label joins the effective keys of action with " / ".
func (r Registry) Label(action Action) string {
	return strings.Join(r.Keys(action), " / ")
}

// Bindings returns the bindings of scope in catalog order, with copied slices.
func (r Registry) Bindings(scope Scope) []Binding {
	bindings := make([]Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		if binding.Scope == scope {
			bindings = append(bindings, cloneBinding(binding))
		}
	}

	return bindings
}

// Configurable returns only the bindings the user may override, in catalog order.
func (r Registry) Configurable() []Binding {
	bindings := make([]Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		if binding.Configurable {
			bindings = append(bindings, cloneBinding(binding))
		}
	}

	return bindings
}

// All returns every binding in catalog order, with copied slices.
func (r Registry) All() []Binding {
	bindings := make([]Binding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		bindings = append(bindings, cloneBinding(binding))
	}

	return bindings
}

// reservedKeys cannot be used by user overrides; the defaults may still use them.
var reservedKeys = map[string]struct{}{
	"ctrl+c": {},
	"esc":    {},
	"enter":  {},
}

// catalog is the single source of the default bindings, in display order.
var catalog = []Binding{
	// root
	{Action: ActionQuit, Scope: ScopeRoot, Keys: []string{"q"}, FixedKeys: []string{"ctrl+c"}, Desc: "salir de la aplicación", Configurable: true},
	{Action: ActionHelp, Scope: ScopeRoot, Keys: []string{"?"}, Desc: "abrir la ayuda", Configurable: true},
	{Action: ActionConfig, Scope: ScopeRoot, Keys: []string{","}, Desc: "ver la configuración", Configurable: true},
	{Action: ActionTabListen, Scope: ScopeRoot, Keys: []string{"1"}, Desc: "pestaña de puertos a la escucha", Configurable: true},
	{Action: ActionTabConnected, Scope: ScopeRoot, Keys: []string{"2"}, Desc: "pestaña de conexiones", Configurable: true},
	{Action: ActionTabAll, Scope: ScopeRoot, Keys: []string{"3"}, Desc: "pestaña con todos los sockets", Configurable: true},
	{Action: ActionFocusNext, Scope: ScopeRoot, Keys: []string{"tab"}, Desc: "siguiente panel", Configurable: true},
	{Action: ActionFocusPrev, Scope: ScopeRoot, Keys: []string{"shift+tab"}, Desc: "panel anterior", Configurable: true},
	{Action: ActionToggleInspector, Scope: ScopeRoot, Keys: []string{"f2"}, Desc: "minimizar o mostrar el detalle", Configurable: true},
	{Action: ActionSplitShrink, Scope: ScopeRoot, Keys: []string{"shift+left"}, Desc: "reducir el ancho de la tabla", Configurable: true},
	{Action: ActionSplitGrow, Scope: ScopeRoot, Keys: []string{"shift+right"}, Desc: "ampliar el ancho de la tabla", Configurable: true},

	// ports
	{Action: ActionPortsUp, Scope: ScopePorts, Keys: []string{"up", "k"}, Desc: "subir", Configurable: true},
	{Action: ActionPortsDown, Scope: ScopePorts, Keys: []string{"down", "j"}, Desc: "bajar", Configurable: true},
	{Action: ActionPortsPageUp, Scope: ScopePorts, Keys: []string{"pgup"}, Desc: "página arriba", Configurable: true},
	{Action: ActionPortsPageDown, Scope: ScopePorts, Keys: []string{"pgdown"}, Desc: "página abajo", Configurable: true},
	{Action: ActionPortsHome, Scope: ScopePorts, Keys: []string{"home"}, Desc: "inicio de la tabla", Configurable: true},
	{Action: ActionPortsEnd, Scope: ScopePorts, Keys: []string{"end"}, Desc: "fin de la tabla", Configurable: true},
	{Action: ActionPortsSort, Scope: ScopePorts, Keys: []string{"s"}, Desc: "cambiar el orden", Configurable: true},
	{Action: ActionPortsFilter, Scope: ScopePorts, Keys: []string{"/"}, Desc: "filtrar", Configurable: true},
	{Action: ActionPortsClear, Scope: ScopePorts, Keys: []string{"esc"}, Desc: "limpiar el filtro", Configurable: true},
	{Action: ActionPortsKill, Scope: ScopePorts, Keys: []string{"x"}, Desc: "matar el proceso con SIGKILL", Configurable: true},
	{Action: ActionPortsTerm, Scope: ScopePorts, Keys: []string{"t"}, Desc: "terminar el proceso con SIGTERM", Configurable: true},
	{Action: ActionPortsRefresh, Scope: ScopePorts, Keys: []string{"r"}, Desc: "refrescar ahora", Configurable: true},

	// inspector
	{Action: ActionInspectorUp, Scope: ScopeInspector, Keys: []string{"up", "k"}, Desc: "subir", Configurable: true},
	{Action: ActionInspectorDown, Scope: ScopeInspector, Keys: []string{"down", "j"}, Desc: "bajar", Configurable: true},
	{Action: ActionInspectorPageUp, Scope: ScopeInspector, Keys: []string{"pgup"}, Desc: "página arriba", Configurable: true},
	{Action: ActionInspectorPageDown, Scope: ScopeInspector, Keys: []string{"pgdown"}, Desc: "página abajo", Configurable: true},
	{Action: ActionInspectorHome, Scope: ScopeInspector, Keys: []string{"home"}, Desc: "inicio del detalle", Configurable: true},
	{Action: ActionInspectorEnd, Scope: ScopeInspector, Keys: []string{"end"}, Desc: "fin del detalle", Configurable: true},

	// filter
	{Action: ActionFilterApply, Scope: ScopeFilter, Keys: []string{"enter"}, Desc: "aplicar el filtro", Configurable: false},
	{Action: ActionFilterCancel, Scope: ScopeFilter, Keys: []string{"esc"}, Desc: "cerrar y limpiar el filtro", Configurable: false},
}

// newRegistry indexes bindings by action and by scope, rejecting duplicates.
func newRegistry(bindings []Binding) (Registry, error) {
	registry := Registry{
		bindings: bindings,
		actions:  make(map[Action]int, len(bindings)),
	}

	for index, binding := range bindings {
		if _, ok := registry.actions[binding.Action]; ok {
			return Registry{}, fmt.Errorf("keymap: acción duplicada %q", binding.Action)
		}

		registry.actions[binding.Action] = index
	}

	scopes, err := indexScopes(bindings)
	if err != nil {
		return Registry{}, err
	}

	registry.scopes = scopes

	return registry, nil
}

// mustRegistry builds a registry and panics only when the static catalog is
// inconsistent, which is a programming error.
func mustRegistry(bindings []Binding) Registry {
	registry, err := newRegistry(bindings)
	if err != nil {
		panic("keymap: catálogo de defaults inválido: " + err.Error())
	}

	return registry
}

// indexScopes maps every effective key of every scope to its action and rejects
// two actions sharing a key inside the same scope.
func indexScopes(bindings []Binding) (map[Scope]map[string]Action, error) {
	scopes := make(map[Scope]map[string]Action, len(bindings))

	for _, binding := range bindings {
		keys := scopes[binding.Scope]
		if keys == nil {
			keys = make(map[string]Action, len(binding.Keys)+len(binding.FixedKeys))
			scopes[binding.Scope] = keys
		}

		for _, key := range effectiveKeys(binding) {
			if previous, ok := keys[key]; ok {
				return nil, fmt.Errorf(
					"keymap: conflicto en el scope %q: la tecla %q está asignada a %q y a %q",
					binding.Scope, key, previous, binding.Action,
				)
			}

			keys[key] = binding.Action
		}
	}

	return scopes, nil
}

// normalizeKeys validates one override list and returns the canonical, unique
// keys in order.
func normalizeKeys(action Action, raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("keymap: la acción %q necesita al menos una tecla", action)
	}

	keys := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))

	for _, item := range raw {
		key, err := ParseKey(item)
		if err != nil {
			return nil, fmt.Errorf("keymap: la acción %q: tecla inválida %q", action, item)
		}

		if _, reserved := reservedKeys[key]; reserved {
			return nil, fmt.Errorf("keymap: la acción %q: la tecla %q está reservada", action, key)
		}

		if _, duplicated := seen[key]; duplicated {
			continue
		}

		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	return keys, nil
}

// effectiveKeys returns the configurable keys followed by the fixed ones. The
// result is always a fresh slice.
func effectiveKeys(binding Binding) []string {
	keys := make([]string, 0, len(binding.Keys)+len(binding.FixedKeys))
	keys = append(keys, binding.Keys...)
	keys = append(keys, binding.FixedKeys...)

	return keys
}

// sortedActions returns the override keys in deterministic order.
func sortedActions(overrides map[string][]string) []string {
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func cloneBindings(bindings []Binding) []Binding {
	if bindings == nil {
		return nil
	}

	cloned := make([]Binding, len(bindings))
	for index, binding := range bindings {
		cloned[index] = cloneBinding(binding)
	}

	return cloned
}

func cloneBinding(binding Binding) Binding {
	binding.Keys = cloneStrings(binding.Keys)
	binding.FixedKeys = cloneStrings(binding.FixedKeys)

	return binding
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}
