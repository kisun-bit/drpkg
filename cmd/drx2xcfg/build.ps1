<#
    drx2xcfg 编译脚本

    支持:
      windows amd64
      windows 386
      linux amd64
      linux arm64

    用法:
      .\build.ps1 windows amd64
#>

param(
    [Parameter(Mandatory=$true)]
    [ValidateSet("windows", "linux")]
    [string]$OS,

    [Parameter(Mandatory=$true)]
    [ValidateSet("amd64", "386", "arm64")]
    [string]$Arch
)

$ErrorActionPreference = "Stop"


# 参数检查
if ($OS -eq "windows" -and $Arch -notin @("amd64", "386")) {
    throw "Windows only supports amd64 and 386"
}

if ($OS -eq "linux" -and $Arch -notin @("amd64", "arm64")) {
    throw "Linux only supports amd64 and arm64"
}


$env:GOOS = $OS
$env:GOARCH = $Arch


$ext = ""

if ($OS -eq "windows") {
    $ext = ".exe"
}


$output = "../../ps/recovery/x2xlib/library/extra/firstboot/drx2xcfg/$OS/$Arch/drx2xcfg$ext"


Write-Host "Building drx2xcfg..."
Write-Host "GOOS=$env:GOOS"
Write-Host "GOARCH=$env:GOARCH"
Write-Host "OUTPUT=$output"


go build `
    -trimpath `
    -o $output


if ($LASTEXITCODE -ne 0) {
    throw "Build failed"
}


Write-Host "Build success: $output"