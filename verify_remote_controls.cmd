@echo off
setlocal

set ROOT_DIR=%~dp0
set HUB_URL=http://127.0.0.1:9399
set PROGRESS_FILE=%ROOT_DIR%.last_remote_demo.json
set INPUT_TEXT=
set EXTRA_FLAGS=

if not "%~1"=="" set HUB_URL=%~1
if not "%~2"=="" set PROGRESS_FILE=%~2
if not "%~3"=="" set INPUT_TEXT=%~3
if /I "%~4"=="interrupt" set EXTRA_FLAGS=-Interrupt
if /I "%~4"=="kill" set EXTRA_FLAGS=-Kill

powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT_DIR%verify_remote_controls.ps1" -HubUrl "%HUB_URL%" -ProgressFile "%PROGRESS_FILE%" -InputText "%INPUT_TEXT%" %EXTRA_FLAGS%
exit /b %ERRORLEVEL%
