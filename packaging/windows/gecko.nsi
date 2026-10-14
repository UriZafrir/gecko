; Gecko installer for Windows. Built by packaging/build.sh, which defines
; VERSION (shown), VIVERSION (a.b.c.d), EXE (the gecko.exe to ship) and
; OUTFILE. Installs for the current user only, so it needs no admin rights.

Unicode true
SetCompressor /SOLID lzma
RequestExecutionLevel user

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Gecko"

Name "Gecko"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Gecko"
InstallDirRegKey HKCU "Software\Gecko" "InstallDir"
BrandingText "Gecko ${VERSION}"

VIProductVersion "${VIVERSION}"
VIAddVersionKey "ProductName" "Gecko"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "Gecko installer"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Gecko contributors"

!include MUI2.nsh

!define MUI_ICON "gecko.ico"
!define MUI_UNICON "gecko.ico"
!define MUI_COMPONENTSPAGE_NODESC
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION OpenGecko
!define MUI_FINISHPAGE_RUN_TEXT "Open Gecko"

!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function OpenGecko
	Exec '"$SYSDIR\wscript.exe" "$INSTDIR\gecko-open.vbs"'
FunctionEnd

Section "Gecko" SecMain
	SectionIn RO
	SetOutPath "$INSTDIR"
	; A running daemon keeps gecko.exe open. Windows still lets a running exe
	; be renamed, so move it aside: the daemon and its sessions keep going,
	; and the next start uses the new version. Old copies still in use stay
	; until a later install or the uninstall.
	IfFileExists "$INSTDIR\gecko.exe" 0 fresh
		System::Call "kernel32::GetTickCount() i .r0"
		Rename "$INSTDIR\gecko.exe" "$INSTDIR\gecko-$0.exe.old"
	fresh:
	Delete "$INSTDIR\gecko-*.exe.old"
	File "/oname=gecko.exe" "${EXE}"
	File "gecko.ico"
	File "gecko-open.vbs"
	File "path.ps1"

	CreateShortcut "$SMPROGRAMS\Gecko.lnk" "$SYSDIR\wscript.exe" '"$INSTDIR\gecko-open.vbs"' "$INSTDIR\gecko.ico"

	WriteUninstaller "$INSTDIR\uninstall.exe"
	WriteRegStr HKCU "Software\Gecko" "InstallDir" "$INSTDIR"
	WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "Gecko"
	WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
	WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "Gecko"
	WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\gecko.ico"
	WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
	WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
	WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
	WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
	WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
	SectionGetSize ${SecMain} $0
	WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" $0
SectionEnd

Section "Add gecko to PATH (for the command line)" SecPath
	nsExec::Exec 'powershell -NoProfile -ExecutionPolicy Bypass -File "$INSTDIR\path.ps1" add "$INSTDIR"'
	Pop $0
	SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
SectionEnd

Section /o "Desktop shortcut" SecDesktop
	CreateShortcut "$DESKTOP\Gecko.lnk" "$SYSDIR\wscript.exe" '"$INSTDIR\gecko-open.vbs"' "$INSTDIR\gecko.ico"
SectionEnd

Section "Uninstall"
	; Uninstalling ends the daemon, and with it the sessions it holds.
	IfFileExists "$INSTDIR\gecko.exe" 0 stopped
		nsExec::Exec '"$INSTDIR\gecko.exe" stop'
		Pop $0
		Sleep 1000
	stopped:
	nsExec::Exec 'powershell -NoProfile -ExecutionPolicy Bypass -File "$INSTDIR\path.ps1" remove "$INSTDIR"'
	Pop $0
	SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000

	Delete "$SMPROGRAMS\Gecko.lnk"
	Delete "$DESKTOP\Gecko.lnk"
	; Left by `gecko service install`.
	Delete "$SMSTARTUP\gecko.vbs"
	Delete "$INSTDIR\gecko.exe"
	Delete "$INSTDIR\gecko-*.exe.old"
	Delete "$INSTDIR\gecko.ico"
	Delete "$INSTDIR\gecko-open.vbs"
	Delete "$INSTDIR\path.ps1"
	Delete "$INSTDIR\uninstall.exe"
	RMDir "$INSTDIR"
	DeleteRegKey HKCU "${UNINST_KEY}"
	DeleteRegKey HKCU "Software\Gecko"
SectionEnd
