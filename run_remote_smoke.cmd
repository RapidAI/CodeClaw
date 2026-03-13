@echo off
setlocal
set GOCACHE=%~dp0.gocache
set GOMODCACHE=%~dp0.gomodcache

if "%~1"=="" (
  echo Usage:
  echo   run_remote_smoke.cmd -- -project D:\path\to\repo -pty-probe -launch-probe
  echo   run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\repo -start -verify-hub
  echo   run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\repo -start -verify-hub -hold-seconds 60
  echo   run_remote_smoke.cmd -- -email you@example.com -hub-url http://127.0.0.1:9399 -center-url http://127.0.0.1:9388 -activate -project D:\path\to\repo -start -verify-hub -hold-seconds 60 -progress-file D:\workprj\aicoder\.last_remote_smoke_live.json
  exit /b 1
)

set ARGS=%*
if "%~1"=="--" (
  set ARGS=%*
  set ARGS=%ARGS:~3%
)

powershell -ExecutionPolicy Bypass -Command "Set-Location '%~dp0'; go run . remote-smoke %ARGS%"
endlocal
