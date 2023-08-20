# define installer name
OutFile "filesyncserviceinstaller.exe"
 
# set desktop as install directory
InstallDir $PROGRAMFILES\FileSyncServer
 
# default section start
Section
 
# define output path
SetOutPath $INSTDIR
 
# specify file to go in output path
File filesyncservice.exe
File filesyncservice.xml
File filesyncserver.exe
File createservercerts.bat
File createclientcert.bat
File openssl.cnf
 
# define uninstaller name
WriteUninstaller $INSTDIR\uninstaller.exe

#install the service
Exec '"$INSTDIR\filesyncservice.exe" install'
#Sleep 1000
#Exec '"$INSTDIR\filesyncservice.exe" start'
 
#-------
# default section end
SectionEnd
 
# create a section to define what the uninstaller does.
# the section will always be named "Uninstall"
Section "Uninstall"

Exec '"$INSTDIR\filesyncservice.exe" stop'
Sleep 1000
Exec '"$INSTDIR\filesyncservice.exe" uninstall'
Sleep 1000

 
# Delete installed files
Delete $INSTDIR\*.exe
Delete $INSTDIR\*.xml
Delete $INSTDIR\*.log
Delete $INSTDIR\*.bat
Delete $INSTDIR\*.cnf
Delete $INSTDIR\*.pem
 
# Delete the uninstaller
Delete $INSTDIR\uninstaller.exe
 
# Delete the directory
RMDir $INSTDIR
SectionEnd