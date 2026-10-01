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
timeout /t 1 >nul

echo [+] Wails Desktop Uygulamasi Derleniyor (Release)...
wails build -ldflags="-s -w -H windowsgui -X github.com/SonKoz-Game/SonKozGlide/internal/updater.Version=v!APPVER!"
if errorlevel 1 goto build_failed

if not exist "build\bin\SonKozGlide.exe" goto build_failed

echo [+] Taze uretilen EXE ana klasore tasiniyor...
move /Y "build\bin\SonKozGlide.exe" ".\"
if errorlevel 1 goto build_failed

echo.
echo [OK] Basariyla derlendi: SonKozGlide.exe   surum v!APPVER!
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
