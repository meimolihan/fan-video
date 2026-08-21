# 本地海报匹配诊断脚本
# 使用方法：
# 1. 保存此脚本为 nfo_debug.ps1
# 2. 执行: .\nfo_debug.ps1 "/test/xxx.mp4"

param(
    [Parameter(Mandatory=$true)]
    [string]$VideoPath
)

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  本地海报匹配诊断工具" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# 处理路径（Windows 兼容）
if ($VideoPath -match "^[A-Za-z]:") {
    $VideoPath = $VideoPath -replace "/", "\"
} else {
    # WSL 或类 Unix 路径，转换为 Windows 路径
    $VideoPath = $VideoPath -replace "^/", "C:\"
    $VideoPath = $VideoPath -replace "/", "\"
}

Write-Host "[输入] 视频路径: $VideoPath" -ForegroundColor Yellow
Write-Host ""

# 检查视频文件是否存在
if (-not (Test-Path $VideoPath)) {
    Write-Host "[错误] 视频文件不存在: $VideoPath" -ForegroundColor Red
    exit 1
}

$VideoDir = Split-Path $VideoPath -Parent
$BaseName = [System.IO.Path]::GetFileNameWithoutExtension($VideoPath)

Write-Host "[阶段1] 视频所在目录: $VideoDir" -ForegroundColor Cyan
Write-Host "[阶段1] 基础文件名: $BaseName" -ForegroundColor Cyan
Write-Host ""

# 检查目录是否存在
if (-not (Test-Path $VideoDir)) {
    Write-Host "[错误] 视频目录不存在: $VideoDir" -ForegroundColor Red
    exit 1
}

# 列出目录内容
Write-Host "--------------------------------------------" -ForegroundColor Gray
Write-Host "[调试] 目录内容:" -ForegroundColor Gray
Get-ChildItem $VideoDir -Force | ForEach-Object {
    $type = if ($_.PSIsContainer) { "[目录]" } else { "[文件]" }
    Write-Host "  $type $($_.Name)"
}
Write-Host ""

# 阶段1：查找同名图片
Write-Host "[阶段1] 查找同名图片（视频目录）..." -ForegroundColor Cyan
$found = $false
$suffixes = @("-poster.jpg", "-poster.png", "-poster.webp", 
              "-cover.jpg", "-cover.png", "-cover.webp",
              "-thumb.jpg", "-thumb.png", "-thumb.webp",
              ".jpg", ".png", ".webp")

foreach ($suffix in $suffixes) {
    $imgPath = Join-Path $VideoDir "$BaseName$suffix"
    if (Test-Path $imgPath) {
        Write-Host "[阶段1] ✅ 找到: $imgPath" -ForegroundColor Green
        $found = $true
        break
    }
}

if (-not $found) {
    Write-Host "[阶段1] ❌ 未找到同名图片" -ForegroundColor Red
}

Write-Host ""

# 阶段1b：子目录中的同名图片
Write-Host "[阶段1b] 查找子目录中的同名图片..." -ForegroundColor Cyan
$subDirs = Get-ChildItem $VideoDir -Directory -Force

if ($subDirs.Count -eq 0) {
    Write-Host "[阶段1b] ❌ 无子目录" -ForegroundColor Red
} else {
    Write-Host "[阶段1b] 子目录列表: $($subDirs.Name -join ', ')" -ForegroundColor Gray
    Write-Host ""

    $found = $false
    $imageExts = @(".jpg", ".jpeg", ".png", ".webp")

    foreach ($sub in $subDirs) {
        Write-Host "[阶段1b] 检查子目录: $($sub.FullName)" -ForegroundColor Gray

        foreach ($ext in $imageExts) {
            $imgPath = Join-Path $sub.FullName "$BaseName$ext"
            if (Test-Path $imgPath) {
                Write-Host "[阶段1b] ✅ 找到: $imgPath" -ForegroundColor Green
                $found = $true
                break
            }
        }

        if ($found) { break }

        # 调试：列出子目录内容
        Write-Host "[阶段1b]   子目录内容:" -ForegroundColor DarkGray
        Get-ChildItem $sub.FullName -Force | Select-Object -First 10 | ForEach-Object {
            Write-Host "[阶段1b]     - $($_.Name)" -ForegroundColor DarkGray
        }
    }

    if (-not $found) {
        Write-Host "[阶段1b] ❌ 未在子目录中找到同名图片" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  诊断结果" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan

if ($found) {
    Write-Host "✅ 海报匹配成功!" -ForegroundColor Green
} else {
    Write-Host "❌ 海报匹配失败!" -ForegroundColor Red

    Write-Host ""
    Write-Host "============================================" -ForegroundColor Cyan
    Write-Host "  修复建议" -ForegroundColor Cyan
    Write-Host "============================================" -ForegroundColor Cyan

    Write-Host ""
    Write-Host "请确保海报文件存在且命名正确：" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  选项1: $($VideoDir)\$($BaseName).jpg" -ForegroundColor White
    Write-Host "         (直接放在视频目录)" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  选项2: $($VideoDir)\任意子目录\$($BaseName).jpg" -ForegroundColor White
    Write-Host "         (放在子目录中，任意子目录名均可)" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "支持的图片格式: .jpg .jpeg .png .webp" -ForegroundColor Gray
    Write-Host ""

    # 列出所有可能的图片位置供参考
    Write-Host "============================================" -ForegroundColor Cyan
    Write-Host "  搜索同名的所有图片文件" -ForegroundColor Cyan
    Write-Host "============================================" -ForegroundColor Cyan
    Get-ChildItem $VideoDir -Recurse -Force -Filter "$BaseName*" | 
        Where-Object { -not $_.PSIsContainer -and $_.Extension -in $imageExts } |
        ForEach-Object {
            Write-Host "  - $($_.FullName)" -ForegroundColor White
        }
}
