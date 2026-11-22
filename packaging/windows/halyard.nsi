; Halyard installer for Windows (NSIS). Build: makensis -DVERSION=1.0.0 -DBINDIR=... halyard.nsi
Unicode true
!ifndef VERSION
  !define VERSION "1.0.0"
!endif
!ifndef BINDIR
  !define BINDIR "."
!endif
!define APP "Halyard"
!define REG "Software\Microsoft\Windows\CurrentVersion\Uninstall\Halyard"

Name "${APP} ${VERSION}"
OutFile "halyard-${VERSION}-setup.exe"
InstallDir "$PROGRAMFILES64\${APP}"
InstallDirRegKey HKLM "Software\${APP}" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma
BrandingText "${APP} ${VERSION}"
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APP}"
VIAddVersionKey "CompanyName" "Fireball Industries"
VIAddVersionKey "FileDescription" "${APP} mail client installer"
VIAddVersionKey "FileVersion" "${VERSION}"
Icon "halyard.ico"
UninstallIcon "halyard.ico"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!define MUI_ICON "halyard.ico"
!define MUI_UNICON "halyard.ico"
!define MUI_FINISHPAGE_RUN "$INSTDIR\halyard.exe"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; The window needs the Microsoft Edge WebView2 runtime. Windows 11 and current Windows 10 have it;
; if it is missing, fetch Microsoft's bootstrapper and run it.
Function EnsureWebView2
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $0 == ""
    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${EndIf}
  ${If} $0 == ""
    DetailPrint "Installing the Microsoft WebView2 runtime"
    NSISdl::download "https://go.microsoft.com/fwlink/p/?LinkId=2124703" "$TEMP\MicrosoftEdgeWebview2Setup.exe"
    Pop $1
    ${If} $1 != "success"
      MessageBox MB_ICONSTOP "Halyard needs the Microsoft WebView2 runtime and it could not be downloaded ($1). Install it from microsoft.com/edge/webview2 and run this installer again."
      Abort
    ${EndIf}
    ExecWait '"$TEMP\MicrosoftEdgeWebview2Setup.exe" /silent /install'
    Delete "$TEMP\MicrosoftEdgeWebview2Setup.exe"
  ${EndIf}
FunctionEnd

Section "Halyard"
  SetOutPath "$INSTDIR"
  Call EnsureWebView2
  File "${BINDIR}\halyard.exe"
  File "${BINDIR}\halyardd.exe"
  File "halyard.ico"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateDirectory "$SMPROGRAMS\${APP}"
  CreateShortCut "$SMPROGRAMS\${APP}\${APP}.lnk" "$INSTDIR\halyard.exe" "" "$INSTDIR\halyard.ico"
  CreateShortCut "$DESKTOP\${APP}.lnk" "$INSTDIR\halyard.exe" "" "$INSTDIR\halyard.ico"
  ; handle mailto: links, and show up under Default apps
  WriteRegStr HKLM "Software\${APP}" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "Software\${APP}\Capabilities" "ApplicationName" "${APP}"
  WriteRegStr HKLM "Software\${APP}\Capabilities" "ApplicationDescription" "Mail client for Google, Microsoft 365 and self-hosted mail"
  WriteRegStr HKLM "Software\${APP}\Capabilities\URLAssociations" "mailto" "Halyard.Url.mailto"
  WriteRegStr HKLM "Software\RegisteredApplications" "${APP}" "Software\${APP}\Capabilities"
  WriteRegStr HKLM "Software\Classes\Halyard.Url.mailto" "" "URL:mailto"
  WriteRegStr HKLM "Software\Classes\Halyard.Url.mailto" "URL Protocol" ""
  WriteRegStr HKLM "Software\Classes\Halyard.Url.mailto\DefaultIcon" "" "$INSTDIR\halyard.ico"
  WriteRegStr HKLM "Software\Classes\Halyard.Url.mailto\shell\open\command" "" '"$INSTDIR\halyard.exe" "%1"'
  ; Add or remove programs
  WriteRegStr HKLM "${REG}" "DisplayName" "${APP}"
  WriteRegStr HKLM "${REG}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${REG}" "Publisher" "Fireball Industries"
  WriteRegStr HKLM "${REG}" "DisplayIcon" "$INSTDIR\halyard.ico"
  WriteRegStr HKLM "${REG}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKLM "${REG}" "NoModify" 1
  WriteRegDWORD HKLM "${REG}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\halyard.exe"
  Delete "$INSTDIR\halyardd.exe"
  Delete "$INSTDIR\halyard.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${APP}\${APP}.lnk"
  RMDir "$SMPROGRAMS\${APP}"
  Delete "$DESKTOP\${APP}.lnk"
  DeleteRegKey HKLM "Software\Classes\Halyard.Url.mailto"
  DeleteRegValue HKLM "Software\RegisteredApplications" "${APP}"
  DeleteRegKey HKLM "Software\${APP}"
  DeleteRegKey HKLM "${REG}"
  ; the person's mail and settings in their profile are left alone
SectionEnd
