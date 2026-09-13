# Build Commands

Run all commands from `checkpoint-app/`.

## Build

Normal release APK: `npm run build:android`
Dev release APK (shows debug/developer surfaces): `npm run build:android:dev`
Normal APK, arm64 only (smaller, faster): `npm run build:android -- -PreactNativeArchitectures=arm64-v8a`
Dev APK, arm64 only: `npm run build:android:dev -- -PreactNativeArchitectures=arm64-v8a`

## Recover from build failures

Clear stale packaging output after a failed build: `Remove-Item -Recurse -Force android\app\build\outputs -ErrorAction SilentlyContinue`
Clear the JS bundle when switching between normal and dev builds: `Remove-Item -Recurse -Force android\app\build\outputs,android\app\build\generated\assets\react -ErrorAction SilentlyContinue`
Full reset (slow; use after native/dependency changes): `& .\android\gradlew.bat clean`

## Install and run

Install the built APK on the connected phone: `& "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe" install -r "android\app\build\outputs\apk\release\app-release.apk"`
Launch the app: `& "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe" shell monkey -p com.checkpoint.pendant 1`
Stream app logs: `& "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe" logcat -s ReactNativeJS:V`
List connected devices: `& "$env:LOCALAPPDATA\Android\Sdk\platform-tools\adb.exe" devices -l`

## Check and test

Typecheck, lint and format check: `npm run check`
Run unit tests: `npm test`
