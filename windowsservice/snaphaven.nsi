# define installer name
OutFile "SnapHavenInstaller.exe"
 
# set program files as install directory
InstallDir $PROGRAMFILES\SnapHavenServer
 
# default section start
Section
 
# define output path
SetOutPath $INSTDIR
 
# specify file to go in output path
File snaphaven.exe
 
# Create a shortcut in the Start Menu Startup folder for auto-launch in tray
CreateShortCut "$SMSTARTUP\SnapHaven Server.lnk" "$INSTDIR\snaphaven.exe"

# Launch application detached without spawning a console window
Exec '"$INSTDIR\snaphaven.exe"'

# define uninstaller name
WriteUninstaller $INSTDIR\uninstaller.exe

SectionEnd
 
# create a section to define what the uninstaller does.
Section "Uninstall"

# 1. Terminate any running instances of SnapHaven server
ExecWait 'taskkill /F /IM snaphaven.exe'
Sleep 500

# 2. Remove Startup shortcut
Delete "$SMSTARTUP\SnapHaven Server.lnk"

# 3. Delete installed files and subdirectories
Delete $INSTDIR\snaphaven.exe
Delete $INSTDIR\uninstaller.exe
Delete $INSTDIR\*.log
Delete $INSTDIR\*.json
Delete $INSTDIR\*.pem
RMDir /r $INSTDIR\certs

# 4. Remove installation folder
RMDir /r $INSTDIR

# 5. Clean up user config directory in AppData (%APPDATA%\SnapHaven)
RMDir /r "$APPDATA\SnapHaven"

SectionEnd
