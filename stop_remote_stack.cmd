@echo off
setlocal

echo.
echo Stopping local CodeClaw remote stack...
echo.

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference = 'SilentlyContinue';" ^
  "$windowPatterns = @('CodeClaw Hub Center*', 'CodeClaw Hub*');" ^
  "foreach ($pattern in $windowPatterns) {" ^
  "  taskkill /FI ('WINDOWTITLE eq ' + $pattern) /T /F | Out-Null" ^
  "}" ^
  "$targets = @(@{ Port = 9388; Label = 'CodeClaw Hub Center' }, @{ Port = 9399; Label = 'CodeClaw Hub' });" ^
  "foreach ($target in $targets) {" ^
  "  $pids = Get-NetTCPConnection -State Listen -LocalPort $target.Port -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique;" ^
  "  if (-not $pids) {" ^
  "    Write-Host ('[INFO] No listening process found on port ' + $target.Port + '.');" ^
  "    continue" ^
  "  }" ^
  "  foreach ($procId in $pids) {" ^
  "    try {" ^
  "      Stop-Process -Id $procId -Force -ErrorAction Stop;" ^
  "      Write-Host ('[OK] Stopped ' + $target.Label + ' process on port ' + $target.Port + ' (PID ' + $procId + ').');" ^
  "    } catch {" ^
  "      Write-Host ('[WARN] Failed to stop ' + $target.Label + ' process on port ' + $target.Port + ' (PID ' + $procId + '): ' + $_.Exception.Message);" ^
  "    }" ^
  "  }" ^
  "}"

echo.
echo CodeClaw remote stack stop sequence finished.
echo.

endlocal
exit /b 0
