@echo off
setlocal EnableDelayedExpansion

REM ==============================================================================
REM == Batch Script to Build and Package the AICoder Application for Windows    ==
REM ==============================================================================

echo [INFO] Starting the build process...

REM -- Set Environment Variables --
set "APP_NAME=AICoder"
set "OUTPUT_DIR=%~dp0dist"
set "NSIS_PATH=C:\Program Files (x86)\NSIS\makensis.exe"

REM -- Ensure Go tools are in PATH --
set "GOPATH=%USERPROFILE%\go"
set "PATH=%GOPATH%\bin;%PATH%"

REM -- Clean previous build artifacts --
echo [Step 1/7] Cleaning previous build...
if exist "%OUTPUT_DIR%" (
    rmdir /s /q "%OUTPUT_DIR%"
)
mkdir "%OUTPUT_DIR%"

REM -- Increment build number and set version --
echo [Step 2/7] Updating version number...
powershell -NoProfile -Command "if (Test-Path 'build_number') { $n = [int](Get-Content 'build_number') + 1 } else { $n = 1 }; Set-Content -Path 'build_number' -Value $n -NoNewline; Set-Content -Path 'temp_build_num.txt' -Value $n -NoNewline"
set /p BUILD_NUM=<temp_build_num.txt
del temp_build_num.txt
set "VERSION=3.8.2.%BUILD_NUM%"
echo [INFO] Building Version: %VERSION% 

REM -- Sync version with frontend --
echo [Step 3/7] Syncing version with frontend...
powershell -Command "$filePath = '%~dp0frontend\src\App.tsx'; $content = [System.IO.File]::ReadAllText($filePath); $newContent = $content -replace 'const APP_VERSION = ".*";', 'const APP_VERSION = "%VERSION%"'; [System.IO.File]::WriteAllText($filePath, $newContent, [System.Text.UTF8Encoding]::new($false))"
powershell -Command "\"export const buildNumber = `\"%BUILD_NUM%`\";\" | Set-Content -Path '%~dp0frontend\src\version.ts' -Encoding Utf8"

REM -- Build Frontend --
echo [Step 4/8] Building frontend...
cd "%~dp0frontend"
call npm.cmd install --cache ./.npm_cache
if !errorlevel! neq 0 (
    echo [ERROR] npm install failed.
    goto :error
)
call npm.cmd run build
if !errorlevel! neq 0 (
    echo [ERROR] Frontend build failed.
    goto :error
)
cd "%~dp0"

REM -- Generate Windows Resources (icon only) --
echo [Step 5/8] Generating Windows resources...
"%GOPATH%\bin\rsrc.exe" -ico "%~dp0build\windows\icon.ico" -arch amd64 -o "%~dp0resource_windows_amd64.syso"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate amd64 resources.
    goto :error
)
"%GOPATH%\bin\rsrc.exe" -ico "%~dp0build\windows\icon.ico" -arch arm64 -o "%~dp0resource_windows_arm64.syso"
if !errorlevel! neq 0 (
    echo [ERROR] Failed to generate arm64 resources.
    goto :error
)

REM -- Build Go Binaries --
echo [Step 6/8] Compiling Go binaries...
set "GOOS=windows"
set "CGO_ENABLED=0"
set "GOARCH=amd64"
go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_amd64.exe"
if !errorlevel! neq 0 (
    echo [ERROR] Go build for amd64 failed.
    goto :error
)
set "GOARCH=arm64"
go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o "%OUTPUT_DIR%\%APP_NAME%_arm64.exe"
if !errorlevel! neq 0 (
    echo [ERROR] Go build for arm64 failed.
    goto :error
)
del "%~dp0resource_windows_amd64.syso"
del "%~dp0resource_windows_arm64.syso"

REM Reset Env for NSIS
set "GOOS="
set "GOARCH="
set "CGO_ENABLED="
set "CC="
set "CXX="

REM -- Create NSIS Installer --
echo [Step 7/7] Creating NSIS installer...
if not exist "%NSIS_PATH%" goto nsis_missing

"%NSIS_PATH%" /DINFO_PRODUCTVERSION="%VERSION%" /DARG_WAILS_AMD64_BINARY="%OUTPUT_DIR%\%APP_NAME%_amd64.exe" /DARG_WAILS_ARM64_BINARY="%OUTPUT_DIR%\%APP_NAME%_arm64.exe" "%~dp0build\windows\installer\multiarch.nsi"
if !errorlevel! neq 0 (
    echo [ERROR] NSIS installer creation failed.
    goto :error
)

if exist "%OUTPUT_DIR%\%APP_NAME%-Setup.exe" (
    echo [SUCCESS] Windows installer created at: %OUTPUT_DIR%\%APP_NAME%-Setup.exe
)

REM -- Copy/Rename Main Binary for convenience --
echo   - Creating main executable copy (amd64)...
copy /Y "%OUTPUT_DIR%\%APP_NAME%_amd64.exe" "%OUTPUT_DIR%\%APP_NAME%.exe" >nul
if exist "%OUTPUT_DIR%\%APP_NAME%.exe" (
    echo [SUCCESS] Windows main binary created: %OUTPUT_DIR%\%APP_NAME%.exe
    
    echo   - Creating Windows portable zip...
    powershell -Command "Compress-Archive -Path '%OUTPUT_DIR%\%APP_NAME%.exe' -DestinationPath '%OUTPUT_DIR%\%APP_NAME%-Windows-Portable.zip' -Force"
)

goto :success

:nsis_missing
echo [ERROR] NSIS not found at "%NSIS_PATH%". Please install NSIS.
goto :error

:success
echo.
echo [SUCCESS] Build and packaging complete!
echo Artifacts are in: %OUTPUT_DIR%
goto :eof

:error
echo.
echo [FAILED] The build process failed. Please check the output above for errors.
pause
exit /b 1

endlocal
