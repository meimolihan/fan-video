# fan-video 本地海报匹配诊断工具
# 
# 使用方法：
# 1. 将此脚本复制到运行 fan-video 的机器上
# 2. 修改下面的配置信息
# 3. 执行: powershell -ExecutionPolicy Bypass -File diagnose_poster.ps1

# ============================================================
# 配置：请根据实际情况修改以下变量
# ============================================================

# 媒体库挂载路径（docker-compose 中映射的路径）
# 格式1：Windows 本地路径（如 D:\Videos）
# 格式2：WSL/Linux 路径（如 /mnt/d/Videos 或 /media）
$MediaMountPath = "/media"

# 视频文件的绝对路径（fan-video 数据库中记录的路径）
# 可以从 Web UI 的媒体详情页获取
$TestMediaPath = "/media/流浪地球/流浪地球.mp4"

# ============================================================
# 诊断脚本
# ============================================================

Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  fan-video 本地海报匹配诊断工具" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# 处理路径
if ($MediaMountPath -match "^[A-Za-z]:") {
    # Windows 路径
    $MediaPath = $MediaMountPath
    $IsWindowsPath = $true
} else {
    # 类 Unix 路径
    $MediaPath = $MediaMountPath -replace "^/", "C:\"
    $MediaPath = $MediaPath -replace "/", "\"
    $IsWindowsPath = $false
}

Write-Host "[配置] 媒体挂载路径: $MediaMountPath" -ForegroundColor Yellow
Write-Host "[配置] 转换后路径: $MediaPath" -ForegroundColor Gray
Write-Host "[配置] 是否为 Windows 路径: $IsWindowsPath" -ForegroundColor Gray
Write-Host ""

# 检查媒体目录是否存在
if (-not (Test-Path $MediaPath)) {
    Write-Host "[错误] 媒体目录不存在: $MediaPath" -ForegroundColor Red
    Write-Host ""
    Write-Host "可能的原因：" -ForegroundColor Yellow
    Write-Host "  1. Docker 容器未启动" -ForegroundColor White
    Write-Host "  2. 路径映射不正确（检查 docker-compose.yml 的 volumes 配置）" -ForegroundColor White
    Write-Host "  3. 宿主机上的媒体目录不存在" -ForegroundColor White
    Write-Host "  4. Docker 没有权限访问该路径" -ForegroundColor White
    exit 1
}

Write-Host "[OK] 媒体目录存在" -ForegroundColor Green
Write-Host ""

# 解析测试路径
if ($TestMediaPath.StartsWith("/")) {
    $TestMediaPathWin = $TestMediaPath -replace "^/", ""
    $TestMediaPathWin = Join-Path $MediaPath $TestMediaPathWin
    $TestMediaPathWin = $TestMediaPathWin -replace "/", "\"
} else {
    $TestMediaPathWin = $TestMediaPath
}

Write-Host "[测试] 视频文件路径: $TestMediaPathWin" -ForegroundColor Yellow
Write-Host ""

# 检查视频文件是否存在
if (-not (Test-Path $TestMediaPathWin)) {
    Write-Host "[错误] 视频文件不存在: $TestMediaPathWin" -ForegroundColor Red
    Write-Host ""
    Write-Host "请检查：" -ForegroundColor Yellow
    Write-Host "  1. 数据库中记录的路径是否正确" -ForegroundColor White
    Write-Host "  2. docker-compose 中 volumes 映射是否正确" -ForegroundColor White
    exit 1
}

Write-Host "[OK] 视频文件存在" -ForegroundColor Green

# 获取基本信息
$VideoDir = Split-Path $TestMediaPathWin -Parent
$BaseName = [System.IO.Path]::GetFileNameWithoutExtension($TestMediaPathWin)
$VideoName = [System.IO.Path]::GetFileName $TestMediaPathWin

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  文件结构分析" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "视频目录: $VideoDir" -ForegroundColor White
Write-Host "基础名称: $BaseName" -ForegroundColor White
Write-Host "视频文件名: $VideoName" -ForegroundColor White
Write-Host ""

# 检查目录权限
Write-Host "[权限检查]" -ForegroundColor Cyan
try {
    $testFile = Join-Path $VideoDir ".poster_test_$(Get-Random)"
    [System.IO.File]::WriteAllText($testFile, "test")
    Remove-Item $testFile -Force
    Write-Host "[OK] 目录可写" -ForegroundColor Green
} catch {
    Write-Host "[警告] 目录可能不可写: $_" -ForegroundColor Yellow
}

try {
    $entries = Get-ChildItem $VideoDir -Force -ErrorAction Stop
    Write-Host "[OK] 目录可读（包含 $($entries.Count) 个项目）" -ForegroundColor Green
} catch {
    Write-Host "[错误] 目录不可读: $_" -ForegroundColor Red
    exit 1
}
Write-Host ""

# 列出目录内容
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  目录内容" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Get-ChildItem $VideoDir -Force | ForEach-Object {
    $type = if ($_.PSIsContainer) { "[目录]" } else { "[文件]" }
    $size = if (-not $_.PSIsContainer) { " ($([Math]::Round($_.Length/1KB, 1)) KB)" } else { "" }
    Write-Host "  $type $($_.Name)$size"
}
Write-Host ""

# 阶段1：查找同名图片
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  阶段1：查找同名图片（视频目录）" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

$found = $false
$suffixes = @("-poster.jpg", "-poster.png", "-poster.webp", 
              "-cover.jpg", "-cover.png", "-cover.webp",
              "-thumb.jpg", "-thumb.png", "-thumb.webp",
              ".jpg", ".png", ".webp")

foreach ($suffix in $suffixes) {
    $imgPath = Join-Path $VideoDir "$BaseName$suffix"
    $exists = Test-Path $imgPath
    $status = if ($exists) { "✅ 找到" } else { "❌ 不存在" }
    Write-Host "  $BaseName$suffix : $status" -ForegroundColor $(if ($exists) { "Green" } else { "DarkGray" })
    if ($exists -and -not $found) {
        $found = $true
        $foundPath = $imgPath
    }
}

if ($found) {
    Write-Host ""
    Write-Host "[结果] ✅ 在视频目录找到海报: $foundPath" -ForegroundColor Green
} else {
    Write-Host ""
    Write-Host "[结果] ❌ 在视频目录未找到海报" -ForegroundColor Red
}
Write-Host ""

# 阶段1b：子目录中的同名图片
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  阶段1b：子目录中的同名图片" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

$subDirs = Get-ChildItem $VideoDir -Directory -Force

if ($subDirs.Count -eq 0) {
    Write-Host "[结果] ❌ 无子目录" -ForegroundColor Red
} else {
    Write-Host "找到 $($subDirs.Count) 个子目录:" -ForegroundColor Gray
    Write-Host ""

    $found = $false
    $imageExts = @(".jpg", ".jpeg", ".png", ".webp")

    foreach ($sub in $subDirs) {
        Write-Host "  检查子目录: $($sub.Name)" -ForegroundColor White

        foreach ($ext in $imageExts) {
            $imgPath = Join-Path $sub.FullName "$BaseName$ext"
            $exists = Test-Path $imgPath
            
            if ($exists) {
                Write-Host "    $BaseName$ext : ✅ 找到!" -ForegroundColor Green
                $found = $true
                $foundPath = $imgPath
                break
            }
        }

        if (-not $found) {
            # 显示子目录内容供参考
            $subContent = Get-ChildItem $sub.FullName -Force -ErrorAction SilentlyContinue
            if ($subContent) {
                $contentList = ($subContent | Select-Object -First 5 | ForEach-Object { $_.Name }) -join ", "
                if ($subContent.Count -gt 5) {
                    $contentList += " ..."
                }
                Write-Host "    子目录内容: $contentList" -ForegroundColor DarkGray
            }
        }
    }

    Write-Host ""
    if ($found) {
        Write-Host "[结果] ✅ 在子目录找到海报: $foundPath" -ForegroundColor Green
    } else {
        Write-Host "[结果] ❌ 在子目录未找到同名图片" -ForegroundColor Red
    }
}
Write-Host ""

# 全局搜索同名文件
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  全局搜索（搜索所有同名图片）" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

$globalResults = Get-ChildItem $VideoDir -Recurse -Force -Filter "$BaseName*" | 
    Where-Object { -not $_.PSIsContainer -and $_.Extension -in $imageExts }

if ($globalResults) {
    Write-Host "找到 $($globalResults.Count) 个同名图片文件:" -ForegroundColor Yellow
    foreach ($r in $globalResults) {
        Write-Host "  - $($r.FullName)" -ForegroundColor White
    }
} else {
    Write-Host "未找到任何同名图片文件" -ForegroundColor Red
}
Write-Host ""

# 总结
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  诊断总结" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

if ($found) {
    Write-Host "✅ 海报匹配应该成功!" -ForegroundColor Green
    Write-Host ""
    Write-Host "如果海报仍然无法显示，可能的原因：" -ForegroundColor Yellow
    Write-Host "  1. 数据库中的路径与实际文件路径不匹配" -ForegroundColor White
    Write-Host "  2. 刮削服务未正确调用 FindLocalImagesForMedia" -ForegroundColor White
    Write-Host "  3. 刮削后数据未正确保存到数据库" -ForegroundColor White
    Write-Host "  4. 前端图片加载路径配置错误" -ForegroundColor White
} else {
    Write-Host "❌ 海报匹配失败!" -ForegroundColor Red
    Write-Host ""
    Write-Host "请确保海报文件存在且命名正确：" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  选项1: $($VideoDir)\$($BaseName).jpg" -ForegroundColor White
    Write-Host "         (直接放在视频目录，与视频平级)" -ForegroundColor Gray
    Write-Host ""
    Write-Host "  选项2: $($VideoDir)\封面图\$($BaseName).jpg" -ForegroundColor White
    Write-Host "         (放在任意子目录中)" -ForegroundColor Gray
    Write-Host ""
    Write-Host "支持的图片格式: .jpg .jpeg .png .webp" -ForegroundColor Gray
}
Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
