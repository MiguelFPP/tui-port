# Changelog

Todos los cambios relevantes de este proyecto se documentan en este archivo.

El formato sigue [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) y el
proyecto usa [Versionado Semántico](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-11-20

Primera versión: monitor de puertos con tabla, detalle del socket y del proceso,
pestañas, orden, filtro y terminación de procesos con guardas.

### Added

- Tabla de puertos con una fila por socket del kernel: `PROTO`, `ENDPOINT`,
  `ESTADO`, `PROCESO` y `RSS`. El endpoint usa `*` para el comodín y corchetes
  para IPv6, y se recorta por la izquierda para conservar el puerto cuando no
  cabe.
- Pestañas `Listen`, `Conectadas` y `Todas`, que filtran el snapshot por
  actividad sin volver a leer `/proc`; la pestaña activa no roba el foco.
- Orden con `s`: ciclo `puerto`, `rss`, `proceso`, `pid`, con desempates
  estables por puerto, protocolo e inode.
- Filtro en vivo con `/` sobre puerto, proceso, pid, protocolo, endpoint, estado
  y usuario, con `enter` para conservar el texto y `esc` para limpiarlo.
- Detalle del socket seleccionado: `proto`, `local`, `remoto`, `estado`, `uid`,
  `inode`, y del proceso dueño: nombre, usuario, ppid, RSS, hilos, inicio con
  antigüedad y cmdline ajustada a 6 líneas como máximo.
- Lista de todos los puertos del proceso dueño, ordenada por puerto local y
  protocolo, con el socket seleccionado marcado.
- `x` envía `SIGKILL` y `t` envía `SIGTERM` al proceso del socket seleccionado.
  Las guardas rechazan `pid <= 0`, `pid 1` y el pid propio, y la tabla no
  intenta la señal cuando el socket no tiene dueño resuelto.
- Ratón: clic para seleccionar filas, pestañas y botón del detalle clicables,
  rueda sobre el panel bajo el cursor y arrastre del borde compartido con
  guardado del ancho al soltar.
- Layout con detalle acotado a un mínimo de 24 celdas y tabla de 40: `f2`
  minimiza el detalle, `shift+left`/`shift+right` mueven el borde y el ancho se
  persiste en `split_width`/`split_collapsed`.
- Cuatro temas: Tokyo Night (default), Catppuccin Mocha, Dracula y Gruvbox Dark.
- Overlay de ayuda (`?`) con las teclas efectivas y overlay read-only de
  configuración (`,`) con archivo, tema, overrides y layout.
- `internal/tui/keymap`: catálogo de acciones por scope (`root`, `ports`,
  `inspector`, `filter`), parser canónico de teclas, validación de overrides,
  detección de conflictos y teclas reservadas (`ctrl+c`, `esc`, `enter`).
- Configuración JSON en `$XDG_CONFIG_HOME/tui-port/config.json`, con
  `--config`, `TUI_PORT_CONFIG`, `--theme` y `TUI_PORT_THEME`. Precedencia
  flags > env > archivo > defaults, lectura estricta, límite de 1 MiB y
  escrituras atómicas con permisos `0600`.
- `--init-config` (con `--force`), `--print-config`, `--list-themes`,
  `--list-keys`, `--version` y `--help`.
- Adaptador `internal/adapters/procfs` sobre Linux: lectura de
  `/proc/net/{tcp,tcp6,udp,udp6}` con decodificación little-endian de IPv4 y de
  los cuatro words de IPv6, mapeo de estados del kernel y resolución del dueño
  por los enlaces `socket:[inode]` de `/proc/<pid>/fd`.
- Metadatos del proceso desde `/proc/<pid>/{status,cmdline,stat}` y `btime` de
  `/proc/stat`, con caché de UID a usuario y límites de dominio para sockets,
  procesos, descriptores, cmdline y tamaño de tabla.
- Stub para sistemas no Linux que implementa el puerto y devuelve
  `domain.ErrUnsupported`, de modo que la TUI muestra `sistema no soportado`.
- Entorno de desarrollo en contenedores: `Dockerfile` multi-stage con Air para
  hot reload, `compose.yaml` con los servicios `dev`, `host` (perfil `host`,
  red y PID del host) y `app` (perfil `prod`), y `Makefile` con targets de
  build, ejecución, test, vet, formato y limpieza.
- Instaladores `scripts/install.sh` (Linux/macOS, POSIX `sh`) y
  `scripts/install.ps1` (Windows): detectan plataforma y arquitectura, resuelven
  la última release o la versión indicada, verifican el SHA-256 contra
  `checksums.txt` y reemplazan el binario por etapas para no dejar una copia a
  medias.
- CI y release: `.github/workflows/ci.yml` (`make test` y `make vet` en push y
  PR a `main` y `develop`) y `.github/workflows/release.yml` (GoReleaser ante un
  tag `v*`), con `.goreleaser.yaml` para linux/darwin/windows × amd64 y arm64,
  `checksums.txt` SHA-256 y la plantilla `config.example.json`.

### Notes

- Sin privilegios, `/proc/net` se lee completo pero `/proc/<pid>/fd` solo se
  puede leer para procesos propios: el resto de sockets queda sin dueño, se
  muestran con `?` en `PROCESO` y `-` en `RSS`, y el detalle sugiere ejecutar
  como root. Con `sudo` o con `CAP_SYS_PTRACE` se atribuyen todos.
- Dentro de un contenedor solo se ve el espacio de nombres del contenedor; el
  perfil `host` de Compose comparte red y PID con el host y la vía recomendada
  para uso real es ejecutar el binario en el host.
- Los sockets UDP ligados (`07`) se renderizan como `listen`, porque el kernel
  usa ese código para datagramas y no hay estado de conexión.
- Un servicio que escucha en IPv4 e IPv6 aparece en dos filas (`tcp` y `tcp6`),
  porque son tablas distintas del kernel.
- Los sockets en `TIME_WAIT` no tienen descriptor abierto y quedan sin proceso.
- `Snapshot.Truncated` se propaga desde el adaptador cuando se alcanza un
  límite, pero ninguna vista lo muestra todavía.
- Los instaladores resuelven la última release publicada;
  `.github/workflows/release.yml` la publica con GoReleaser cuando se empuja un
  tag `vX.Y.Z`.

[Unreleased]: https://github.com/MiguelFPP/tui-port/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/MiguelFPP/tui-port/releases/tag/v0.1.0
