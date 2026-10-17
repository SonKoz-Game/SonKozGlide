@echo off
setlocal enabledelayedexpansion
color 0A
cd /d "%~dp0"

echo [==============================]
echo [     SonKoz Glide Build       ]
echo [==============================]
echo.
echo Kullanim: build.bat [surum]   ornek: build.bat 1.1.0
echo.

if "%~1"=="" goto read_version

echo [+] Surum guncelleniyor: %~1
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\version.ps1" -Set "%~1" >nul
if errorlevel 1 goto build_failed

:read_version
for /f "usebackq delims=" %%v in (`powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\version.ps1"`) do set "APPVER=%%v"

if "!APPVER!"=="" goto no_version

echo [+] Derlenecek surum: v!APPVER!
echo.

echo [+] Arka plandaki eski surumler kapatiliyor...
taskkill /F /IM SonKozGlide.exe >nul 2>&1
taskkill /F /IM SonKozGlide_x86.exe >nul 2>&1
timeout /t 1 >nul

set "LDFLAGS=-s -w -H windowsgui -X github.com/SonKoz-Game/SonKozGlide/internal/updater.Version=v!APPVER!"

echo [+] 64-bit surum derleniyor (windows/amd64)...
wails build -platform windows/amd64 -ldflags="!LDFLAGS!"
if errorlevel 1 goto build_failed
if not exist "build\bin\SonKozGlide.exe" goto build_failed
move /Y "build\bin\SonKozGlide.exe" ".\"
if errorlevel 1 goto build_failed

echo [+] 32-bit surum derleniyor (windows/386)...
wails build -platform windows/386 -o SonKozGlide_x86.exe -ldflags="!LDFLAGS!"
if errorlevel 1 goto build_failed
if not exist "build\bin\SonKozGlide_x86.exe" goto build_failed
move /Y "build\bin\SonKozGlide_x86.exe" ".\"
if errorlevel 1 goto build_failed

echo.
echo [OK] Basariyla derlendi: SonKozGlide.exe ve SonKozGlide_x86.exe   surum v!APPVER!
echo [OK] Uygulama bu klasorde hazir, cift tiklayinca pencereli modern arayuz acilacak!
echo.
pause
exit /b 0

:no_version
echo.
color 0C
echo [FAIL] wails.json icinden surum okunamadi.
echo.
pause
exit /b 1

:build_failed
echo.
color 0C
echo [FAIL] Derleme sirasinda bir hata olustu.
echo [FAIL] Wails, NPM ve Go kurulumlarini kontrol edin.
echo.
pause
exit /b 1
