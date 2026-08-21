# fan-video Phase 1 cleanup script
# Run: cd C:\Users\meimo\Downloads\test\fan-video; .\cleanup_phase1.ps1

$base = $PSScriptRoot
$deleted = 0

Write-Host "=== Phase 1 Cleanup ===" -ForegroundColor Cyan

# 1. cmd/ directories
$cmdDirs = @(
    'clean-dirty', 'dedupe-movies', 'diagnose', 'fix-tvshow-merge',
    'transcode-encoder-time-base-cert', 'transcode-encoder-time-base-reorder-cert',
    'transcode-fixture-cert', 'transcode-long-duration-drift-cert',
    'transcode-long-duration-profile-cert', 'transcode-long-duration-scaling-cert',
    'transcode-output-cadence-cert', 'transcode-real-media-candidate-cert',
    'transcode-real-media-corpus-generate', 'transcode-real-media-corpus-spec',
    'transcode-recovery-stress-cert', 'transcode-source-origin-cert',
    'transcode-vfr-isolation-cert'
)
foreach ($d in $cmdDirs) {
    $path = Join-Path $base "cmd\$d"
    if (Test-Path $path) {
        Remove-Item -Path $path -Recurse -Force
        Write-Host "  DEL cmd/$d/"
        $deleted++
    }
}

# 2. scripts/ files (keep docker-entrypoint.sh, run-server.bat)
$keepScripts = @('docker-entrypoint.sh', 'run-server.bat')
Get-ChildItem (Join-Path $base 'scripts') -File | Where-Object { $_.Name -notin $keepScripts } | ForEach-Object {
    Remove-Item -Path $_.FullName -Force
    Write-Host "  DEL scripts/$($_.Name)"
    $deleted++
}

# 3. .github/workflows/ (android/desktop related)
$wfFiles = Get-ChildItem (Join-Path $base '.github\workflows') -File | Where-Object {
    $_.Name -match 'android|desktop|release-android|release-desktop'
}
foreach ($f in $wfFiles) {
    Remove-Item -Path $f.FullName -Force
    Write-Host "  DEL .github/workflows/$($f.Name)"
    $deleted++
}

# 4. config/ai.yaml
$aiYaml = Join-Path $base 'config\ai.yaml'
if (Test-Path $aiYaml) {
    Remove-Item -Path $aiYaml -Force
    Write-Host "  DEL config/ai.yaml"
    $deleted++
}

Write-Host ""
Write-Host "Done. Deleted: $deleted items" -ForegroundColor Green
