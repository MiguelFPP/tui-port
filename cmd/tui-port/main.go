package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/MiguelFPP/tui-port/internal/adapters/procfs"
	"github.com/MiguelFPP/tui-port/internal/config"
	"github.com/MiguelFPP/tui-port/internal/ports"
	"github.com/MiguelFPP/tui-port/internal/tui/app"
	"github.com/MiguelFPP/tui-port/internal/tui/keymap"
	"github.com/MiguelFPP/tui-port/internal/tui/styles"
)

// version and commit are overridden at build time with
// -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "0.1.0"
	commit  = "dev"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, config.ErrHelp) {
			fmt.Fprintln(os.Stdout, config.Usage)
			return
		}

		fmt.Fprintf(os.Stderr, "tui-port: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}

	if cfg.ShowVersion {
		fmt.Fprintln(os.Stdout, versionLine())

		return nil
	}

	// The catalogs are static: listing them neither reads the config file nor
	// starts the TUI.
	if cfg.ListThemes {
		printThemes(os.Stdout)

		return nil
	}
	if cfg.ListKeys {
		printKeys(os.Stdout)

		return nil
	}

	// --init-config writes the template and exits; the path goes to stdout so it
	// can be piped, and no socket read runs.
	if cfg.InitConfig {
		if err := config.WriteDefault(cfg.ConfigPath, cfg.Force); err != nil {
			return err
		}

		fmt.Fprintln(os.Stdout, cfg.ConfigPath)

		return nil
	}

	// --print-config dumps the resolved values without starting the TUI.
	if cfg.PrintConfig {
		if err := printConfig(os.Stdout, cfg); err != nil {
			return err
		}

		return nil
	}

	keys, err := keymap.Default().Apply(cfg.Keys)
	if err != nil {
		return fmt.Errorf("aplicar atajos: %w", err)
	}

	// The palette is settled before the adapter so every style token is ready
	// when the views render.
	theme := styles.DefaultTheme()
	if cfg.Theme != "" {
		var ok bool
		theme, ok = styles.ThemeByName(cfg.Theme)
		if !ok {
			return fmt.Errorf("tema desconocido %q; disponibles: %s", cfg.Theme, strings.Join(styles.ThemeNames(), ", "))
		}
	}
	styles.Apply(theme)

	// A missing /proc is not fatal: the table shows an actionable state instead.
	var monitor ports.MonitorService
	if service, err := procfs.New(""); err == nil {
		monitor = service
	}

	deps := app.Deps{Config: cfg, Monitor: monitor, Keys: keys}
	if cfg.SavePath != "" {
		deps.SaveSplit = func(width int, collapsed bool) error {
			return config.SaveSplit(cfg.SavePath, width, collapsed)
		}
	}

	program := tea.NewProgram(app.New(deps))
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("ejecutar la TUI: %w", err)
	}

	return nil
}

// versionLine prints tui-port <version> and appends the commit when one was
// injected by the release build; local builds keep the default "dev" marker.
func versionLine() string {
	if commit == "" || commit == "dev" {
		return "tui-port " + version
	}

	return fmt.Sprintf("tui-port %s (%s)", version, commit)
}

// printThemes writes the theme catalog, one row per theme, flagging the default.
func printThemes(w io.Writer) {
	if w == nil {
		w = io.Discard
	}

	def := styles.DefaultTheme().Name
	for _, theme := range styles.Themes() {
		suffix := ""
		if theme.Name == def {
			suffix = " (default)"
		}

		fmt.Fprintf(w, "%-20s %s%s\n", theme.Name, theme.Description, suffix)
	}
}

// printKeys writes the default bindings, one row per action, with the effective
// keys. User overrides are not applied on purpose: this documents the defaults.
func printKeys(w io.Writer) {
	if w == nil {
		w = io.Discard
	}

	keys := keymap.Default()

	fmt.Fprintln(w, "accion\tscope\tteclas\ttipo")
	for _, binding := range keys.All() {
		kind := "configurable"
		if !binding.Configurable {
			kind = "fija"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", binding.Action, binding.Scope, keys.Label(binding.Action), kind)
	}
}

// printConfig writes the resolved configuration as indented JSON. It only reads
// cfg: the theme is validated against the catalog but the palette and the
// keymap are not applied, because no view is rendered.
func printConfig(w io.Writer, cfg config.Config) error {
	if w == nil {
		w = io.Discard
	}

	name := strings.TrimSpace(cfg.Theme)
	if name == "" {
		name = styles.DefaultTheme().Name
	}
	if _, ok := styles.ThemeByName(name); !ok {
		return fmt.Errorf("tema desconocido %q; disponibles: %s", name, strings.Join(styles.ThemeNames(), ", "))
	}

	// A fresh, never-nil map keeps the JSON stable: with no overrides the output
	// is "keys": {} instead of null.
	keys := make(map[string][]string, len(cfg.Keys))
	for action, list := range cfg.Keys {
		keys[action] = append([]string{}, list...)
	}

	out := struct {
		ConfigPath     string              `json:"config_path"`
		Theme          string              `json:"theme"`
		SplitWidth     int                 `json:"split_width"`
		SplitCollapsed bool                `json:"split_collapsed"`
		Keys           map[string][]string `json:"keys"`
	}{
		ConfigPath:     cfg.ConfigPath,
		Theme:          name,
		SplitWidth:     max(cfg.SplitWidth, 0),
		SplitCollapsed: cfg.SplitCollapsed,
		Keys:           keys,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("imprimir la configuración: %w", err)
	}

	return nil
}
