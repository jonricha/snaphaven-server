!include "MUI2.nsh"

Name "SnapHaven Server"
OutFile "SnapHavenInstaller.exe"
InstallDir "$PROGRAMFILES\SnapHavenServer"

Var SYNCDIR

; --- MUI Settings ---
!define MUI_ABORTWARNING

; --- Pages ---
!insertmacro MUI_PAGE_WELCOME

; Page 1: Application Installation Directory
!insertmacro MUI_PAGE_DIRECTORY

; Page 2: Media Sync Directory Selection Page
!define MUI_DIRECTORYPAGE_VARIABLE $SYNCDIR
!define MUI_DIRECTORYPAGE_TEXT_TOP "Choose the folder where SnapHaven Server will store synced media files (photos and videos)."
!define MUI_DIRECTORYPAGE_TEXT_DESTINATION "Media Sync Directory"
!insertmacro MUI_PAGE_DIRECTORY

!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

; Uninstaller Pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Languages
!insertmacro MUI_LANGUAGE "English"

Function .onInit
  StrCpy $SYNCDIR "$PROFILE\snaphaven"
FunctionEnd

Function EscapeJsonString
  Exch $0 ; original string
  Push $1 ; char
  Push $2 ; len
  Push $3 ; index
  Push $4 ; result string

  StrCpy $4 ""
  StrLen $2 $0
  StrCpy $3 0

  loop:
    IntCmp $3 $2 done done
    StrCpy $1 $0 1 $3
    IntOp $3 $3 + 1
    StrCmp $1 "\" 0 append
    StrCpy $1 "\\"
  append:
    StrCpy $4 "$4$1"
    Goto loop

  done:
    Pop $3
    Pop $2
    Pop $1
    Exch $4
FunctionEnd

Section "MainSection" SEC01
  # Terminate any existing running instances of SnapHaven Server
  ExecWait 'taskkill /F /IM snaphaven.exe'
  Sleep 1000

  SetOutPath $INSTDIR
  File snaphaven.exe

  # Ensure AppData configuration directory exists
  CreateDirectory "$APPDATA\SnapHaven"

  # Create sync target directory if it doesn't exist
  CreateDirectory "$SYNCDIR"

  # Escape backslashes for JSON compatibility
  Push $SYNCDIR
  Call EscapeJsonString
  Pop $0

  # Write initial config.json with chosen sync directory
  FileOpen $1 "$APPDATA\SnapHaven\config.json" w
  FileWrite $1 '{\r\n'
  FileWrite $1 '  "sync_directory": "$0",\r\n'
  FileWrite $1 '  "grpc_port": ":50005",\r\n'
  FileWrite $1 '  "cert_directory": "certs",\r\n'
  FileWrite $1 '  "auto_start_on_boot": false,\r\n'
  FileWrite $1 '  "open_browser_on_launch": false\r\n'
  FileWrite $1 '}\r\n'
  FileClose $1

  # Create a shortcut in the Start Menu Startup folder for auto-launch in tray
  CreateShortCut "$SMSTARTUP\SnapHaven Server.lnk" "$INSTDIR\snaphaven.exe"

  # Launch application detached without spawning a console window
  Exec '"$INSTDIR\snaphaven.exe"'

  # Write uninstaller
  WriteUninstaller "$INSTDIR\uninstaller.exe"
SectionEnd

Section "Uninstall"
  # 1. Terminate any running instances of SnapHaven server
  ExecWait 'taskkill /F /IM snaphaven.exe'
  Sleep 500

  # 2. Remove Startup shortcut
  Delete "$SMSTARTUP\SnapHaven Server.lnk"

  # 3. Delete installed files and subdirectories
  Delete "$INSTDIR\snaphaven.exe"
  Delete "$INSTDIR\uninstaller.exe"
  Delete "$INSTDIR\*.log"
  Delete "$INSTDIR\*.json"
  Delete "$INSTDIR\*.pem"
  RMDir /r "$INSTDIR\certs"

  # 4. Remove installation folder
  RMDir /r "$INSTDIR"

  # 5. Clean up user config directory in AppData (%APPDATA%\SnapHaven)
  RMDir /r "$APPDATA\SnapHaven"
SectionEnd
