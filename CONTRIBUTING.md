# Contribuir a tui-port

Gracias por ayudar a mejorar `tui-port`. Esta guía cubre el entorno local, el
flujo de ramas y PRs, los tests y las reglas de commit.

## Requisitos

- Docker
- Docker Compose v2 o superior

Go no se instala en el host. Todo build, test, formato y comando de módulos se
ejecuta dentro del contenedor `dev` a través del `Makefile`. El `README.md`
resume la arquitectura y las decisiones de diseño; este repositorio guarda el
código, el `Makefile`, los workflows y la documentación pública.

## Setup

```bash
git clone git@github.com:MiguelFPP/tui-port.git
cd tui-port
make build     # construye las imágenes dev y runtime
make dev       # TUI con hot reload (necesita TTY)
```

`make dev` ejecuta Air dentro del contenedor y reconstruye al guardar. `make run`
usa la imagen de producción y `make host` comparte la red y los procesos del
host. Los tres necesitan un terminal real: son `docker compose run`, no
`docker compose up -d`.

`make help` lista todos los targets.

## Ramas y pull requests

- `main` es la rama estable; `develop` es la rama de integración.
- Nombres de rama: `feat/fNN-slug`, `fix/slug`, `chore/slug`,
  `release/vX.Y.Z`, `hotfix/slug`.
- Todo cambio entra por pull request hacia `develop`.
- `main` solo recibe pull requests desde `release/*` o `hotfix/*`.
- El check requerido para mergear es el job `test` de GitHub Actions, que
  ejecuta `make test` y `make vet`.
- No hacer push directo a ramas protegidas.
- Los tags `vX.Y.Z` los crea el mantenedor: `.github/workflows/release.yml`
  publica la release con GoReleaser al empujarlos.

## Commits

- Usar [Conventional Commits](https://www.conventionalcommits.org/): `feat:`,
  `fix:`, `docs:`, `chore:`, `test:`, `ci:`.
- Mensajes enfocados; un cambio lógico por commit.
- No agregar el trailer `Co-authored-by`: el autor es el contribuyente.

## Tests

La suite completa (`make test`, `make vet`, `make test-race`) está reservada al
CI y a la fase final. Mientras se implementa un cambio, se ejecutan solo los
paquetes que toca:

```bash
make test-pkg PKG='./internal/<pkg>/...'
make test-run PKG='./internal/<pkg>/...' RUN='TestNombre'
make vet-pkg  PKG='./internal/<pkg>/...'
make bench-pkg PKG='./internal/<pkg>/...'
```

`PKG` y `RUN` son obligatorios donde aplica: si faltan, el target falla con un
mensaje claro.

- Los tests de UI usan el monitor falso de
  `internal/tui/components/porttable` y no abren una TTY.
- Los tests de `internal/adapters/procfs` construyen un montaje `/proc` falso en
  `t.TempDir()`; nunca leen el `/proc` del host.
- Los tests de `internal/config` aíslan `XDG_CONFIG_HOME` para que un archivo
  real no se cuele, y cubren FIFO, `/dev/zero`, symlinks y permisos.
- El archivo `service_linux_test.go` lleva build tag `linux` y no corre en otros
  sistemas.
- `make test-race` necesita `gcc` y `musl-dev`, que la imagen `dev` ya trae; se
  ejecuta solo al cerrar la fase.
- `make vuln` ejecuta `govulncheck`; `make fuzz-pkg` ejecuta un objetivo de
  fuzzing con `PKG`, `FUZZ` y `FUZZTIME` (por defecto `10s`).

## Editar código

- Nunca ejecutar herramientas de Go en el host; usar los targets del contenedor.
- Comentarios mínimos y factuales: explican el porqué de una decisión, no narran
  el código.
- Agregar una dependencia solo con justificación, fijando la versión exacta en
  `go.mod`, y después ejecutar `make tidy` y `make build`.
- El binario es estático: nada que requiera cgo.
- Las señales a procesos pasan por `MonitorService.Kill` con un
  `domain.ProcessRef` (PID + `starttime`); no invocar `kill(1)` ni construir
  comandos de shell.
- Todo texto derivado de `/proc` o de la configuración que se renderice pasa por
  `internal/tui/textsafe`; no confiar en que un nombre de proceso sea imprimible.
- Cualquier lectura nueva de `/proc` debe estar acotada por las constantes de
  `internal/domain/limits.go`.

## Docs e idioma

- Código, nombres de paquetes y documentación técnica en inglés.
- Mensajes de UI y errores en español.
- Documentar solo hechos verificados: no inventar flags, rutas ni resultados de
  tests.
- Las versiones se consultan en `go.mod`; no duplicarlas en la documentación.

## Licencia

Al contribuir aceptas que tu trabajo se publica bajo la licencia MIT (ver
`LICENSE`).
