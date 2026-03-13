@echo off
setlocal

set ROOT_DIR=%~dp0

set EMAIL=%~1
set PROJECT_DIR=%~2
set HOLD_SECONDS=%~3
set PROGRESS_FILE=%~4
set EXTRA_ARGS=%~5

call "%ROOT_DIR%run_full_remote_demo.cmd" "%EMAIL%" "%PROJECT_DIR%" "%HOLD_SECONDS%" "%PROGRESS_FILE%" "%EXTRA_ARGS%" auto-open
exit /b %errorlevel%
