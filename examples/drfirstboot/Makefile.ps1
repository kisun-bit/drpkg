# ============================================================
# Makefile.ps1 — drfirstboot 构建脚本 (PowerShell, Windows)
#
# drfirstboot 必须使用 Go 1.20 (go1.20 或 go1.20.x) 编译,
# 因此不纳入仓库根目录的 build.go (要求 Go >= 1.24),
# 而由本目录的脚本单独编译.
#
# 用法 (在 cmd/drfirstboot 目录执行):
#   .\Makefile.ps1                        编译所有支持平台 (交叉编译)
#   .\Makefile.ps1 all -GOOS linux        仅编译 linux 的所有架构
#   .\Makefile.ps1 all -GOOS linux -GOARCH amd64   仅编译指定平台
#   .\Makefile.ps1 clean                  清理编译产物
#
# 支持平台:
#   windows: 386, amd64
#   linux:   amd64, arm64, 386, loong64
#
# 工具链查找顺序:
#   1. -GO120 参数或环境变量 GO120 (可执行文件名或完整路径)
#   2. PATH 中第一个匹配 go1.20* 的可执行文件 (go1.20 / go1.20.x)
# 无论来源如何, 编译前都会强制校验版本为 go1.20 或 go1.20.x.
#
# 产物输出到 <仓库根>\build\drfirstboot\{OS}\{ARCH}\,
# 与根目录 build.go 的输出布局一致.
# ============================================================

param(
    [Parameter(Position = 0)]
    [ValidateSet('all', 'clean', 'help')]
    [string]$Target = 'all',

    # 按 GOOS 过滤构建矩阵 (为空表示不过滤).
    [string]$GOOS = '',

    # 按 GOARCH 过滤构建矩阵 (为空表示不过滤).
    [string]$GOARCH = '',

    # Go 1.20 工具链 (可执行文件名或完整路径);
    # 未指定时依次使用环境变量 GO120、PATH 中的 go1.20*.
    [string]$GO120 = ''
)

$ErrorActionPreference = 'Stop'

# 构建矩阵: drfirstboot 支持的目标平台.
$AllPlatforms = @(
    'windows/386',
    'windows/amd64',
    'linux/amd64',
    'linux/arm64',
    'linux/386',
    'linux/loong64'
)

# 产物根目录: <仓库根>/build/drfirstboot (与根目录 build.go 布局一致).
$BuildRoot = Join-Path (Resolve-Path '..\..') 'build\drfirstboot'

# Find-Go120 查找 Go 1.20 工具链, 找不到时返回 $null.
function Find-Go120([string]$Preferred) {
    if ($Preferred) {
        return $Preferred
    }

    if ($env:GO120) {
        return $env:GO120
    }

    $cmd = Get-Command -Name 'go1.20*' -ErrorAction SilentlyContinue |
        Select-Object -First 1

    if ($cmd) {
        return $cmd.Source
    }

    return $null
}

# Test-Go120Version 校验工具链版本为 go1.20 或 go1.20.x;
# 通过返回版本信息字符串, 否则返回 $null.
function Test-Go120Version([string]$Go) {
    try {
        $verOut = & $Go version 2>&1 | Select-Object -First 1
    }
    catch {
        $verOut = $null
    }

    if ($verOut -and $verOut -match '^go version go1\.20(\.\d+)?\s') {
        return ([string]$verOut).Trim()
    }

    return $null
}

# Show-Usage 输出用法说明.
function Show-Usage {
    $script = $MyInvocation.ScriptName
    Write-Host @"
用法: .\Makefile.ps1 [目标] [选项]

目标:
  all       编译所有支持平台 (默认)
  clean     清理编译产物
  help      显示本帮助

选项:
  -GOOS <os>       按操作系统过滤构建矩阵 (windows / linux)
  -GOARCH <arch>   按架构过滤构建矩阵 (386 / amd64 / arm64 / loong64)
  -GO120 <path>    指定 Go 1.20 工具链 (可执行文件名或完整路径)

示例:
  .\Makefile.ps1
  .\Makefile.ps1 all -GOOS windows
  .\Makefile.ps1 all -GOOS linux -GOARCH amd64 -GO120 go1.20.14
"@
}

switch ($Target) {
    'help' {
        Show-Usage
        return
    }

    'clean' {
        if (Test-Path $BuildRoot) {
            Remove-Item -Recurse -Force $BuildRoot
        }
        Write-Host ">>> $BuildRoot removed."
        return
    }

    'all' {
        # 1. 查找并校验 Go 1.20 工具链.
        $go = Find-Go120 $GO120

        if (-not $go) {
            Write-Error '未找到 Go 1.20 工具链 (go1.20 或 go1.20.x)。请安装后重试，例如: go install golang.org/dl/go1.20@latest; go1.20 download，或通过 -GO120 <可执行文件> / 环境变量 GO120 指定工具链。'
        }

        $version = Test-Go120Version $go

        if (-not $version) {
            $actual = $null
            try { $actual = (& $go version 2>&1 | Select-Object -First 1) } catch { }
            Write-Error ("要求 Go 1.20 (go1.20 或 go1.20.x)，实际为: {0}。当前工具链: {1}" -f $(if ($actual) { $actual } else { '无法获取版本信息' }), $go)
        }

        Write-Host ">>> 工具链检查通过: $version"

        # 2. 按 GOOS / GOARCH 过滤构建矩阵.
        $platforms = $AllPlatforms
        if ($GOOS) {
            $platforms = $platforms | Where-Object { $_ -like "$GOOS/*" }
        }
        if ($GOARCH) {
            $platforms = $platforms | Where-Object { $_ -like "*/$GOARCH" }
        }

        if (-not $platforms) {
            Write-Error "构建矩阵中不存在 GOOS=$GOOS GOARCH=$GOARCH 的平台。支持的平台: $($AllPlatforms -join ', ')"
        }

        # 3. 逐平台交叉编译.
        #    保存并在编译后恢复环境变量, 避免污染当前会话.
        $envKeys = @('GOOS', 'GOARCH', 'GO111MODULE', 'GOWORK', 'CGO_ENABLED')
        $savedEnv = @{}

        foreach ($p in $platforms) {
            $goos, $goarch = $p -split '/'

            $name = 'drfirstboot'
            if ($goos -eq 'windows') {
                $name += '.exe'
            }

            $out = Join-Path $BuildRoot "$goos\$goarch\$name"
            $outDir = Split-Path $out
            if (-not (Test-Path $outDir)) {
                New-Item -ItemType Directory -Force -Path $outDir | Out-Null
            }

            Write-Host ">>> Building drfirstboot for $goos/$goarch ..."

            foreach ($k in $envKeys) {
                $savedEnv[$k] = [Environment]::GetEnvironmentVariable($k)
            }

            $env:GOOS = $goos
            $env:GOARCH = $goarch
            $env:GO111MODULE = 'on'
            $env:GOWORK = 'off'
            $env:CGO_ENABLED = '0'

            & $go build -trimpath -o $out .
            $code = $LASTEXITCODE

            foreach ($k in $envKeys) {
                if ($null -eq $savedEnv[$k]) {
                    Remove-Item "Env:$k" -ErrorAction SilentlyContinue
                }
                else {
                    Set-Item "Env:$k" $savedEnv[$k]
                }
            }

            if ($code -ne 0) {
                Write-Error "drfirstboot for $p build failed: exit status $code"
            }

            Write-Host ">>> Built drfirstboot for $goos/$goarch => $out"
        }

        Write-Host '>>> All builds completed successfully.'
    }
}

