// Package config parses flags, environment defaults and the optional JSON file.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Config is the validated runtime configuration of the TUI. ConfigPath and Keys
// come from the optional JSON file and Theme may stay empty for main to apply
// its default. SplitWidth and SplitCollapsed hold the persisted table layout,
// while SavePath carries the resolved path where it can be written even before
// the file exists. ShowVersion, ListThemes and ListKeys ask main to print a
// catalog and exit; InitConfig writes a template and PrintConfig dumps the
// resolved values, so neither the file nor the other flags are validated for
// them.
type Config struct {
	ShowVersion    bool
	ListThemes     bool
	ListKeys       bool
	InitConfig     bool
	Force          bool
	PrintConfig    bool
	ConfigPath     string
	Theme          string
	Keys           map[string][]string
	SplitWidth     int
	SplitCollapsed bool
	SavePath       string
}

// Usage is the help text shown when -h or --help is requested.
const Usage = `usage: tui-port [flags]

flags:
  --version          print the version and exit
  --list-themes      lista los temas disponibles y sale
  --list-keys        lista las acciones y sus teclas por defecto y sale
  --config FILE      archivo de configuración JSON, default: $XDG_CONFIG_HOME/tui-port/config.json
  --theme NAME       tema de color, default: tokyonight-night
  --init-config      escribe un archivo de configuración base y sale
  --force            sobrescribe el archivo existente con --init-config
  --print-config     imprime la configuración resuelta y sale`

// ErrHelp reports that -h or --help was requested.
var ErrHelp = flag.ErrHelp

// Errors returned by Parse and by the writers of the package.
var (
	ErrConfigFile   = errors.New("no se pudo leer el archivo de configuración")
	ErrConfigFormat = errors.New("el archivo de configuración no es válido")
)

// Parse validates args, the argument list without the program name, together
// with the environment read through getenv. It returns the resulting
// configuration and never writes to stdout or stderr.
func Parse(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	flags := flag.NewFlagSet("tui-port", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	showVersion := flags.Bool("version", false, "muestra la versión y sale")
	listThemes := flags.Bool("list-themes", false, "lista los temas disponibles y sale")
	listKeys := flags.Bool("list-keys", false, "lista las acciones y sus teclas y sale")
	initConfig := flags.Bool("init-config", false, "escribe un archivo de configuración base y sale")
	force := flags.Bool("force", false, "sobrescribe el archivo existente con --init-config")
	printConfig := flags.Bool("print-config", false, "imprime la configuración resuelta y sale")
	configPath := flags.String("config", strings.TrimSpace(getenv("TUI_PORT_CONFIG")), "archivo de configuración JSON")
	theme := flags.String("theme", strings.TrimSpace(getenv("TUI_PORT_THEME")), "tema de color")

	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if *showVersion {
		return Config{ShowVersion: true}, nil
	}
	// The catalogs are static, so neither the file nor the other flags matter.
	if *listThemes || *listKeys {
		return Config{ListThemes: *listThemes, ListKeys: *listKeys}, nil
	}
	if flags.NArg() > 0 {
		return Config{}, fmt.Errorf("argumento no esperado %q", flags.Arg(0))
	}

	// --init-config needs no existing file and wins over --print-config.
	if *initConfig {
		path, err := resolveInitPath(*configPath)
		if err != nil {
			return Config{}, err
		}

		return Config{InitConfig: true, Force: *force, ConfigPath: path}, nil
	}

	// --print-config and the TUI share the very same loading rules, so the dump
	// never shows a configuration the UI would not use.
	cfg, err := loadConfig(*configPath, *theme)
	if err != nil {
		return Config{}, err
	}
	cfg.PrintConfig = *printConfig

	return cfg, nil
}

// loadConfig resolves the path, reads the JSON file and applies the theme
// precedence: flag, then env (already folded into the flag default), then the
// file. ConfigPath is only set when a file was actually loaded, while SavePath
// always carries the resolved path so the UI can persist the layout.
func loadConfig(configPath, theme string) (Config, error) {
	path, explicit := resolveConfigPath(configPath)
	fileCfg, loaded, err := loadFile(path, explicit)
	if err != nil {
		return Config{}, err
	}
	if fileCfg.SplitWidth < 0 {
		return Config{}, fmt.Errorf("%w: split_width no puede ser negativo", ErrConfigFormat)
	}

	effectiveTheme := strings.TrimSpace(theme)
	if effectiveTheme == "" {
		effectiveTheme = strings.TrimSpace(fileCfg.Theme)
	}

	cfg := Config{
		Theme:          effectiveTheme,
		Keys:           fileCfg.keyOverrides(),
		SplitWidth:     fileCfg.SplitWidth,
		SplitCollapsed: fileCfg.SplitCollapsed,
	}
	if loaded {
		cfg.ConfigPath = path
	}
	if path != "" {
		cfg.SavePath = path
	}

	return cfg, nil
}

// resolveInitPath returns the destination of --init-config. Unlike loadConfig it
// never touches the filesystem, because the file is expected to be missing.
func resolveInitPath(fromFlag string) (string, error) {
	path, _ := resolveConfigPath(fromFlag)
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: no se pudo resolver la ruta de destino; usa --config", ErrConfigFile)
	}

	return path, nil
}
