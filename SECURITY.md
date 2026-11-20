# Política de seguridad

## Reportar una vulnerabilidad

Reporta las vulnerabilidades sospechosas en privado mediante las
[security advisories](https://github.com/MiguelFPP/tui-port/security/advisories/new)
de GitHub, no en un issue público.

Incluye la versión afectada (`tui-port --version`), una descripción del problema
y, cuando sea posible, una reproducción mínima. Recibirás una respuesta inicial
cuando el reporte esté triageado.

## Versiones soportadas

| Versión | Soportada |
| :--- | :--- |
| 0.1.x | Sí |
| anteriores | No |

Solo la última línea de release recibe correcciones de seguridad.

## Alcance: terminar procesos es la funcionalidad

`tui-port` envía `SIGKILL` (`x`) y `SIGTERM` (`t`) al proceso dueño del socket
seleccionado. **Eso es intencionado**: es la razón de existir de la herramienta y
no es una vulnerabilidad por sí mismo.

Lo que sí es materia de seguridad es todo lo que puede ampliar ese poder o
saltarse las guardas:

- Que una señal alcance un proceso distinto del dueño del socket seleccionado.
- Que se pueda enviar una señal sin selección, con un `pid` inventado o a un
  proceso protegido (`pid <= 0`, `pid 1`, el propio pid).
- Que una lectura de `/proc` sin límites permita agotar memoria o bloquear la
  interfaz.

Las guardas viven en el adaptador (`Service.Kill`) y en la tabla
(`startAction`): se aplican antes de llegar al kernel, y una negativa del kernel
(`EPERM`, `ESRCH`) se traduce a un error de dominio que la UI muestra en el pie.

## El binario no necesita root

`tui-port` arranca sin privilegios. Lo que cambia es cuánto puede atribuir:
leer `/proc/<pid>/fd` para resolver el inode de un socket solo está permitido
para procesos propios, así que sin privilegios el resto de filas quedan con `?`
en `PROCESO` y `-` en `RSS`.

Con root, o con la capacidad `CAP_SYS_PTRACE`, se atribuyen todos los procesos
del sistema. Es la misma comprobación de acceso que usa `ptrace`.

> **Advertencia:** ejecutar la TUI con `sudo` da a un programa interactivo acceso
> de lectura a los descriptores de todos los procesos y capacidad de terminarlos.
> Úsalo solo en máquinas propias y con el binario descargado de una fuente
> verificada.

## Ejecutar binarios de terceros

Los artefactos de release incluyen `checksums.txt` con el SHA-256, según
`.goreleaser.yaml`. Verifica el checksum antes de ejecutar cualquier binario
descargado: un `tui-port` alterado puede enviar señales a cualquier proceso al
que tengas acceso.

## `make host`: red y PID del host

El servicio `host` de `compose.yaml` (perfil `host`, target de Make `make host`)
usa `network_mode: host` y `pid: host`. Sirve para probar la imagen contra el
host, pero **no le da privilegios**: el contenedor corre como el usuario `app`
(uid 10001), así que la atribución y las señales siguen limitadas a los procesos
de ese uid en el host. Para operar sobre todos los procesos hay que ejecutar el
binario en el host con root o `CAP_SYS_PTRACE`. No uses el perfil `host` con
imágenes que no hayas construido tú, y no lo dejes corriendo por costumbre:
`make run` (perfil `prod`) es el modo acotado, que solo ve el espacio de nombres
del contenedor.

## Sin red y sin telemetría

`tui-port` no abre conexiones de red, no envía telemetría y no escribe fuera de
la ruta de configuración resuelta. Los únicos datos personales que toca son los
que ya están en `/proc` (pid, usuario, línea de comandos) y solo los muestra en
pantalla.

## Límites de lectura

Las lecturas de `/proc` están acotadas en número de sockets, procesos,
descriptores por proceso, bytes de `status`/`stat`/`cmdline` y tamaño de cada
tabla de red; un montaje hostil o gigante no reserva memoria sin límite. Los
enlaces de `/proc/<pid>/fd` se resuelven con `Readlink`: se lee el destino, no se
abre el archivo apuntado, y solo se acepta el formato `socket:[<inode>]`.

## Actualizaciones de dependencias

Las dependencias se fijan en `go.mod`/`go.sum` y se actualizan por el flujo de
revisión. Reporta una dependencia transitiva vulnerable igual que cualquier otro
hallazgo.
