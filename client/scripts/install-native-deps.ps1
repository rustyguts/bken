$ErrorActionPreference = "Stop"

$requiredPkgConfigModules = @(
  "portaudio-2.0",
  "opus"
)

$msysPackages = @(
  "mingw-w64-x86_64-gcc",
  "mingw-w64-x86_64-pkg-config",
  "mingw-w64-x86_64-portaudio",
  "mingw-w64-x86_64-opus"
)

function Write-Info {
  param([string]$Message)
  Write-Host "[deps] $Message"
}

function Add-UserPathEntry {
  param([string]$Entry)

  $current = [Environment]::GetEnvironmentVariable("Path", "User")
  if ([string]::IsNullOrWhiteSpace($current)) {
    [Environment]::SetEnvironmentVariable("Path", $Entry, "User")
    return
  }

  $parts = $current.Split(";") | Where-Object { $_ -ne "" }
  if ($parts -contains $Entry) {
    return
  }

  [Environment]::SetEnvironmentVariable("Path", "$current;$Entry", "User")
}

$msysRoot = "C:\msys64"
$msysBash = Join-Path $msysRoot "usr\bin\bash.exe"

if (-not (Test-Path $msysBash)) {
  if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    throw "MSYS2 not found and winget is unavailable. Install MSYS2 manually from https://www.msys2.org/"
  }

  Write-Info "Installing MSYS2 via winget..."
  winget install --id MSYS2.MSYS2 -e --accept-package-agreements --accept-source-agreements
}

if (-not (Test-Path $msysBash)) {
  throw "MSYS2 install did not produce $msysBash"
}

Write-Info "Installing Windows native dependencies with MSYS2..."
$packageList = $msysPackages -join " "
& $msysBash -lc "pacman -Sy --noconfirm --needed $packageList"

$mingwBin = "C:\msys64\mingw64\bin"
$pkgConfigPath = "C:\msys64\mingw64\lib\pkgconfig"

Add-UserPathEntry -Entry $mingwBin
[Environment]::SetEnvironmentVariable("PKG_CONFIG_PATH", $pkgConfigPath, "User")

$pkgConfigModules = $requiredPkgConfigModules -join " "
& $msysBash -lc "export PATH=/mingw64/bin:`$PATH; export PKG_CONFIG_PATH=/mingw64/lib/pkgconfig; pkg-config --exists $pkgConfigModules"
if ($LASTEXITCODE -ne 0) {
  throw "pkg-config metadata check failed for one or more required modules: $pkgConfigModules"
}

Write-Info "Native development dependencies are installed."
Write-Info "Open a new terminal, then run from client/: wails dev"
