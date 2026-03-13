@echo off
setlocal

set "HUBCENTER_URL=http://127.0.0.1:9388"
set "HUB_URL=http://127.0.0.1:9399"

if not "%~1"=="" set "HUBCENTER_URL=%~1"
if not "%~2"=="" set "HUB_URL=%~2"

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0run_remote_stack.ps1" ^
  -HubCenterUrl "%HUBCENTER_URL%" ^
  -HubUrl "%HUB_URL%"

endlocal
exit /b %ERRORLEVEL%
