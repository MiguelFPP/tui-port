<#
.SYNOPSIS
    Installer for tui-port on Windows.

.DESCRIPTION
    Downloads the release archive for the current architecture, verifies its
    SHA-256 checksum and installs tui-port into a directory on the user PATH.

.EXAMPLE
    irm https://raw.githubusercontent.com/MiguelFPP/tui-port/main/scripts/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Version v0.1.0 -Dir C:\Tools\tui-port
#>
[CmdletBinding()]
param(
    [string]$Version,
    [string]$Dir,
    [switch]$NoVerify,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Repo = 'MiguelFPP/tui-port'
$Releases = "https://github.com/$Repo/releases"
$Binary = 'tui-port'

function Write-Step {
    param([string]$Message)
    Write-Host "-> $Message" -ForegroundColor Cyan
}

function Write-Warn {
    param([string]$Message)
    Write-Host "tui-port: $Message" -ForegroundColor Yellow
}

function Show-Usage {
    $text = @'
Instala tui-port en Windows.

Uso:
  .\install.ps1 [-Version <vX.Y.Z>] [-Dir <ruta>] [-NoVerify] [-Help]

Opciones:
  -Version <vX.Y.Z>  Versión a instalar (por defecto, la última release).
  -Dir <ruta>        Directorio de instalación (por defecto
                     %LOCALAPPDATA%\Programs\tui-port).
  -NoVerify          Omite la verificación del checksum SHA-256.
  -Help              Muestra esta ayuda.

Ejemplos:
  irm https://raw.githubusercontent.com/MiguelFPP/tui-port/main/scripts/install.ps1 | iex
  .\install.ps1 -Version v0.1.0 -Dir C:\Tools\tui-port

Nota: en Linux, atribuir el proceso dueño de cada puerto requiere ejecutar como
root o con CAP_SYS_PTRACE; sin ese permiso solo se atribuyen tus propios
procesos y el resto aparece sin PID. Para ver todos los puertos con su proceso
usa:
  sudo tui-port
'@
    Write-Host $text
}

function Resolve-Architecture {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }

    switch ($arch) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "arquitectura no soportada: $arch (se soportan amd64 y arm64)" }
    }
}

function Resolve-Version {
    if ($Version) {
        $candidate = $Version.Trim().TrimStart('v')
    }
    else {
        $latest = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
            -Headers @{ 'User-Agent' = 'tui-port-installer' }
        $candidate = $latest.tag_name.Trim().TrimStart('v')
    }

    if ($candidate -notmatch '^[0-9]+\.[0-9]+\.[0-9]+$') {
        throw "versión inválida: $candidate (se espera vX.Y.Z)"
    }

    return $candidate
}

function Resolve-InstallDir {
    if ($Dir) {
        return $Dir
    }

    if ($env:LOCALAPPDATA) {
        return (Join-Path $env:LOCALAPPDATA 'Programs\tui-port')
    }

    if ($env:USERPROFILE) {
        return (Join-Path $env:USERPROFILE '.tui-port\bin')
    }

    return (Join-Path ([System.IO.Path]::GetTempPath()) 'tui-port')
}

function Add-ToUserPath {
    param([string]$Target)

    $normalized = $Target.TrimEnd('\', '/')
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $current) {
        $current = ''
    }

    $entries = $current -split ';' | Where-Object { $_ -ne '' }
    foreach ($entry in $entries) {
        if ($entry.TrimEnd('\', '/') -ieq $normalized) {
            return $false
        }
    }

    $updated = (@($entries) + $normalized) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')

    return $true
}

if ($Help) {
    Show-Usage
    exit 0
}

try {
    Write-Step 'detectando plataforma'
    $arch = Resolve-Architecture

    Write-Step 'resolviendo versión'
    $resolved = Resolve-Version
    $name = "${Binary}_${resolved}_windows_${arch}"
    $url = "$Releases/download/v$resolved/$name.zip"

    $target = Resolve-InstallDir

    Write-Host "  versión:    $resolved"
    Write-Host "  plataforma: windows/$arch"
    Write-Host "  destino:    $target"

    $work = Join-Path ([System.IO.Path]::GetTempPath()) ("tui-port-" + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $work -Force | Out-Null

    try {
        Write-Step "descargando $name.zip"
        $archive = Join-Path $work "$name.zip"
        try {
            Invoke-WebRequest -Uri $url -OutFile $archive -UseBasicParsing
        }
        catch {
            throw "falló la descarga: $url (¿está publicada v$resolved? ver $Releases)"
        }

        if (-not $NoVerify) {
            $checksums = Join-Path $work 'checksums.txt'
            $downloaded = $true
            try {
                Invoke-WebRequest -Uri "$Releases/download/v$resolved/checksums.txt" `
                    -OutFile $checksums -UseBasicParsing
            }
            catch {
                $downloaded = $false
                Write-Warn 'checksums.txt no está disponible; se omite la verificación'
            }

            if ($downloaded) {
                $expected = $null
                foreach ($line in Get-Content $checksums) {
                    $parts = $line -split '\s+', 2
                    if ($parts.Count -eq 2 -and $parts[1].Trim() -eq "$name.zip") {
                        $expected = $parts[0].Trim()
                        break
                    }
                }

                if (-not $expected) {
                    Write-Warn "$name.zip no aparece en checksums.txt; se omite la verificación"
                }
                else {
                    Write-Step 'verificando checksum'
                    $actual = (Get-FileHash -Path $archive -Algorithm SHA256).Hash.ToLowerInvariant()
                    if ($actual -ne $expected.ToLowerInvariant()) {
                        throw "el checksum no coincide para $name.zip (esperado $expected, obtenido $actual)"
                    }
                }
            }
        }

        Write-Step 'extrayendo'
        $extracted = Join-Path $work 'extract'
        Expand-Archive -Path $archive -DestinationPath $extracted -Force

        $exe = Join-Path $extracted "$Binary.exe"
        if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) {
            throw "no se encontró $Binary.exe en el archivo"
        }

        New-Item -ItemType Directory -Path $target -Force | Out-Null
        $destination = Join-Path $target "$Binary.exe"

        # Copy next to the target and move it into place: Windows refuses to
        # overwrite a running executable, and a failed move leaves the previous
        # working binary untouched.
        $staged = Join-Path $target ".$Binary.new.$PID.exe"
        try {
            Copy-Item -Path $exe.FullName -Destination $staged -Force
            Move-Item -Path $staged -Destination $destination -Force
        }
        finally {
            Remove-Item -Path $staged -Force -ErrorAction SilentlyContinue
        }

        Write-Host "instalado $Binary $resolved en $destination" -ForegroundColor Green

        if (Add-ToUserPath -Target $target) {
            Write-Warn "se añadió $target al PATH de usuario; abre una terminal nueva para usar $Binary"
        }

        try {
            & $destination --version
        }
        catch {
            Write-Warn "no se pudo ejecutar $destination --version"
        }
    }
    finally {
        Remove-Item -Path $work -Recurse -Force -ErrorAction SilentlyContinue
    }
}
catch {
    Write-Host "tui-port: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
