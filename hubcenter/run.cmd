@echo off
setlocal
set SCRIPT_DIR=%~dp0
if not exist "%SCRIPT_DIR%configs\config.yaml" (
  copy /Y "%SCRIPT_DIR%configs\config.example.yaml" "%SCRIPT_DIR%configs\config.yaml" >nul
)
powershell -NoProfile -ExecutionPolicy Bypass -Command "Set-Location '%SCRIPT_DIR%'; $env:GOCACHE='%SCRIPT_DIR%.gocache'; $env:GOMODCACHE='%SCRIPT_DIR%.gomodcache'; go run .\cmd\hubcenter --config .\configs\config.yaml"
