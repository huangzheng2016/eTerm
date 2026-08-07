$ErrorActionPreference = "Stop"
$Owner = "huangzheng2016"
$Repo = "eTerm"
if (-not [Environment]::Is64BitOperatingSystem) {
    Write-Error "Only 64-bit Windows is supported."
    exit 1
}
$api = "https://api.github.com/repos/$Owner/$Repo/releases/latest"
$rel = Invoke-RestMethod -Uri $api
$tag = $rel.tag_name
if (-not $tag) {
    Write-Error "Could not read latest release tag."
    exit 1
}
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$zip = "eterm_windows_${arch}.zip"
$url = "https://github.com/$Owner/$Repo/releases/download/$tag/$zip"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("eterm-inst-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $zipPath = Join-Path $tmp $zip
    Invoke-WebRequest -Uri $url -OutFile $zipPath
    Expand-Archive -Path $zipPath -DestinationPath $tmp -Force
    $src = Join-Path $tmp "eterm_windows_${arch}.exe"
    $destDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:USERPROFILE "bin" }
    New-Item -ItemType Directory -Force -Path $destDir | Out-Null
    $dest = Join-Path $destDir "eterm.exe"
    Copy-Item -Path $src -Destination $dest -Force
    Write-Host "Installed: $dest"
    Write-Host 'Add this directory to PATH if eterm is not found:'
    Write-Host "  $destDir"
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
