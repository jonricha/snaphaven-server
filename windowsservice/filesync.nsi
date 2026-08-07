# define installer name
OutFile "PhotoSyncInstaller_v8.exe"
 
# set program files as install directory
InstallDir $PROGRAMFILES\FileSyncServer
 
# default section start
Section
 
# define output path
SetOutPath $INSTDIR
 
# specify file to go in output path
File photosync_server.exe
 
# Create a shortcut in the Start Menu Startup folder for auto-launch in tray
CreateShortCut "$SMSTARTUP\PhotoSync Server.lnk" "$INSTDIR\photosync_server.exe"

# Launch application detached without spawning a console window
Exec '"$INSTDIR\photosync_server.exe"'

# define uninstaller name
WriteUninstaller $INSTDIR\uninstaller.exe

SectionEnd
 
# create a section to define what the uninstaller does.
Section "Uninstall"

# 1. Terminate any running instances of PhotoSync server
ExecWait 'taskkill /F /IM photosync_server.exe'
Sleep 500

# 2. Remove Startup shortcut
Delete "$SMSTARTUP\PhotoSync Server.lnk"

# 3. Delete installed files and subdirectories
Delete $INSTDIR\photosync_server.exe
Delete $INSTDIR\uninstaller.exe
Delete $INSTDIR\*.log
Delete $INSTDIR\*.json
Delete $INSTDIR\*.pem
RMDir /r $INSTDIR\certs

# 4. Remove installation folder
RMDir /r $INSTDIR

# 5. Clean up user config directory in AppData (%APPDATA%\PhotoSync)
RMDir /r "$APPDATA\PhotoSync"

SectionEnd