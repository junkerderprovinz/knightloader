Unicode true

# Wails fills in the project's name, version and binaries in wails_tools.nsh,
# which it writes on every -nsis build. This file is Wails' own template with a
# page for choosing the shortcuts, installed for all users.

# Under Program Files, like any other program. Nobody but an administrator may
# write there, so a scheduled task running as the system account keeps it
# current; see docs/installing.md.
!define REQUEST_EXECUTION_LEVEL "admin"
!define WAILS_INSTALL_SCOPE "machine"
!define UNINST_KEY_NAME "${INFO_PRODUCTNAME}"

!define TASK_NAME "${INFO_PRODUCTNAME} Update"

!include "wails_tools.nsh"
!include "LogicLib.nsh"
!include "Sections.nsh"
!include "StrFunc.nsh"

${StrStr}

# Wails' own name for the entry runs company and product together, and
# wails.json leaves the company to default to the product. An installation
# under that name sits either under Program Files\KnightLoader\KnightLoader for
# all users or under AppData\Local\Programs\KnightLoader for one.
!define LEGACY_KEY_NAME "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
!define LEGACY_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${LEGACY_KEY_NAME}"
!define LEGACY_CHOICE_KEY "Software\${LEGACY_KEY_NAME}"

# Under the all-users context wails.setShellContext sets, $APPDATA is
# ProgramData. The folder holds the update switch and the task's log, and never
# the downloads, settings or accounts, which stay in each user's own folder.
!define DATA_DIR "$APPDATA\${INFO_PRODUCTNAME}"

VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_FINISHPAGE_NOAUTOCLOSE
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_NODESC

# No directory page: the task runs the program as the system account, and a
# folder a user could write to would let that user run anything that way.
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "German"

LangString StartMenuShortcut ${LANG_ENGLISH} "Start menu entry"
LangString StartMenuShortcut ${LANG_GERMAN}  "Eintrag im Startmenü"
LangString DesktopShortcut   ${LANG_ENGLISH} "Shortcut on the desktop"
LangString DesktopShortcut   ${LANG_GERMAN}  "Verknüpfung auf dem Desktop"
LangString TaskFailed        ${LANG_ENGLISH} "Could not set up the background updates. ${INFO_PRODUCTNAME} works, but stays at this version."
LangString TaskFailed        ${LANG_GERMAN}  "Die Updates im Hintergrund ließen sich nicht einrichten. ${INFO_PRODUCTNAME} läuft, bleibt aber auf dieser Version."

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
# The 64-bit Program Files, which is also where Windows on ARM keeps its own
# programs.
InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}"
ShowInstDetails show

# The shortcuts somebody chose. An update installs over the old version, often
# silently, and must not bring back a shortcut they left out.
!define CHOICE_KEY "Software\${UNINST_KEY_NAME}"

Section "-${INFO_PRODUCTNAME}"
    !insertmacro wails.setShellContext
    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR
    # A running copy cannot be overwritten but can be renamed. It steps aside
    # as .old, as it does for an update, and the next update removes it.
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}.old"
    Rename "$INSTDIR\${PRODUCT_EXECUTABLE}" "$INSTDIR\${PRODUCT_EXECUTABLE}.old"
    !insertmacro wails.files
    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols
    !insertmacro wails.writeUninstaller
    # The program compares its own folder with this to tell the installed copy
    # from a portable one.
    WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"

    Call RemoveUserInstall
    Call PrepareData
    Call CreateTask
SectionEnd

Section "$(StartMenuShortcut)" SecStartMenu
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

Section "$(DesktopShortcut)" SecDesktop
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
SectionEnd

# Last, so it sees the final choice: a shortcut left out on a reinstall is
# removed rather than kept from the version before.
Section "-remember the shortcuts"
    ${If} ${SectionIsSelected} ${SecStartMenu}
        WriteRegDWORD SHCTX "${CHOICE_KEY}" "StartMenuShortcut" 1
    ${Else}
        WriteRegDWORD SHCTX "${CHOICE_KEY}" "StartMenuShortcut" 0
        Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    ${EndIf}
    ${If} ${SectionIsSelected} ${SecDesktop}
        WriteRegDWORD SHCTX "${CHOICE_KEY}" "DesktopShortcut" 1
    ${Else}
        WriteRegDWORD SHCTX "${CHOICE_KEY}" "DesktopShortcut" 0
        Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    ${EndIf}
SectionEnd

# The copy installed for the person running this goes, together with its
# shortcuts and its entry under Apps. Only the files an installation and its
# updates put there are deleted, since the folder could have been chosen by
# hand. Downloads, settings and accounts in %APPDATA%\KnightLoader stay.
Function RemoveUserInstall
    ClearErrors
    ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "DisplayIcon"
    ${If} ${Errors}
        Return
    ${EndIf}
    ${GetParent} $0 $1

    # A copy that is still running goes at the next restart.
    Delete /REBOOTOK "$0"
    Delete /REBOOTOK "$0.old"
    Delete "$1\uninstall.exe"
    Delete "$1\.*.update-*"
    RMDir /REBOOTOK "$1"

    SetShellVarContext current
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    SetShellVarContext all

    DeleteRegKey HKCU "${LEGACY_UNINST_KEY}"
    DeleteRegKey HKCU "${LEGACY_CHOICE_KEY}"
FunctionEnd

# The folder under ProgramData belongs to the administrators, and only
# settings.json is open to every user, so the settings page can change the
# switch. Anything else a user could write there, the task running as the
# system account could be tricked into writing somewhere else. What an earlier
# folder held is cleared, keeping only the switch.
Function PrepareData
    StrCpy $3 "true"
    ClearErrors
    FileOpen $0 "${DATA_DIR}\settings.json" r
    ${IfNot} ${Errors}
        FileRead $0 $1
        FileClose $0
        ${StrStr} $2 $1 "false"
        ${If} $2 != ""
            StrCpy $3 "false"
        ${EndIf}
    ${EndIf}

    # A junction in place of the folder would lead everything below elsewhere.
    System::Call 'kernel32::GetFileAttributesW(w "${DATA_DIR}") i .r0'
    ${If} $0 <> -1
        IntOp $0 $0 & 0x400
        ${If} $0 <> 0
            RMDir "${DATA_DIR}"
        ${EndIf}
    ${EndIf}

    Delete "${DATA_DIR}\*.*"
    CreateDirectory "${DATA_DIR}"
    nsExec::ExecToLog 'icacls "${DATA_DIR}" /setowner *S-1-5-32-544'
    Pop $0
    nsExec::ExecToLog 'icacls "${DATA_DIR}" /inheritance:r /grant:r *S-1-5-18:(OI)(CI)F *S-1-5-32-544:(OI)(CI)F *S-1-5-32-545:(OI)(CI)RX'
    Pop $0

    FileOpen $0 "${DATA_DIR}\settings.json" w
    FileWrite $0 '{"autoUpdate":$3}$\r$\n'
    FileClose $0
    nsExec::ExecToLog 'icacls "${DATA_DIR}\settings.json" /grant *S-1-5-32-545:(R,W)'
    Pop $0
FunctionEnd

# Once a day and five minutes after the computer starts, as the system
# account, the way browsers keep themselves current. schtasks reads the
# definition as UTF-16.
Function CreateTask
    InitPluginsDir
    FileOpen $0 "$PLUGINSDIR\task.xml" w
    FileWriteWord $0 0xFEFF
    FileWriteUTF16LE $0 `<?xml version="1.0" encoding="UTF-16"?>$\r$\n`
    FileWriteUTF16LE $0 `<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">$\r$\n`
    FileWriteUTF16LE $0 `  <RegistrationInfo>$\r$\n`
    FileWriteUTF16LE $0 `    <Author>${INFO_COMPANYNAME}</Author>$\r$\n`
    FileWriteUTF16LE $0 `    <Description>Installs new versions of ${INFO_PRODUCTNAME} from its releases on GitHub. Uninstalling ${INFO_PRODUCTNAME} removes this task.</Description>$\r$\n`
    FileWriteUTF16LE $0 `  </RegistrationInfo>$\r$\n`
    FileWriteUTF16LE $0 `  <Triggers>$\r$\n`
    FileWriteUTF16LE $0 `    <BootTrigger>$\r$\n`
    FileWriteUTF16LE $0 `      <Delay>PT5M</Delay>$\r$\n`
    FileWriteUTF16LE $0 `    </BootTrigger>$\r$\n`
    FileWriteUTF16LE $0 `    <CalendarTrigger>$\r$\n`
    FileWriteUTF16LE $0 `      <StartBoundary>2000-01-01T12:00:00</StartBoundary>$\r$\n`
    FileWriteUTF16LE $0 `      <RandomDelay>PT1H</RandomDelay>$\r$\n`
    FileWriteUTF16LE $0 `      <ScheduleByDay>$\r$\n`
    FileWriteUTF16LE $0 `        <DaysInterval>1</DaysInterval>$\r$\n`
    FileWriteUTF16LE $0 `      </ScheduleByDay>$\r$\n`
    FileWriteUTF16LE $0 `    </CalendarTrigger>$\r$\n`
    FileWriteUTF16LE $0 `  </Triggers>$\r$\n`
    FileWriteUTF16LE $0 `  <Principals>$\r$\n`
    FileWriteUTF16LE $0 `    <Principal id="System">$\r$\n`
    FileWriteUTF16LE $0 `      <UserId>S-1-5-18</UserId>$\r$\n`
    FileWriteUTF16LE $0 `      <RunLevel>HighestAvailable</RunLevel>$\r$\n`
    FileWriteUTF16LE $0 `    </Principal>$\r$\n`
    FileWriteUTF16LE $0 `  </Principals>$\r$\n`
    FileWriteUTF16LE $0 `  <Settings>$\r$\n`
    FileWriteUTF16LE $0 `    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>$\r$\n`
    FileWriteUTF16LE $0 `    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>$\r$\n`
    FileWriteUTF16LE $0 `    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>$\r$\n`
    FileWriteUTF16LE $0 `    <StartWhenAvailable>true</StartWhenAvailable>$\r$\n`
    FileWriteUTF16LE $0 `    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>$\r$\n`
    FileWriteUTF16LE $0 `  </Settings>$\r$\n`
    FileWriteUTF16LE $0 `  <Actions Context="System">$\r$\n`
    FileWriteUTF16LE $0 `    <Exec>$\r$\n`
    FileWriteUTF16LE $0 `      <Command>"$INSTDIR\${PRODUCT_EXECUTABLE}"</Command>$\r$\n`
    FileWriteUTF16LE $0 `      <Arguments>--update</Arguments>$\r$\n`
    FileWriteUTF16LE $0 `    </Exec>$\r$\n`
    FileWriteUTF16LE $0 `  </Actions>$\r$\n`
    FileWriteUTF16LE $0 `</Task>$\r$\n`
    FileClose $0

    nsExec::ExecToLog 'schtasks /Create /TN "${TASK_NAME}" /XML "$PLUGINSDIR\task.xml" /F'
    Pop $0
    ${If} $0 != 0
        DetailPrint "$(TaskFailed)"
    ${EndIf}
FunctionEnd

Function .onInit
    !insertmacro wails.checkArchitecture
    !insertmacro wails.setShellContext
    SetRegView 64

    # An installation for all users under Wails' own name has its folder
    # inside this one. Its uninstaller hands itself to a copy and returns at
    # once, so the wait is for its registry entry to go.
    ClearErrors
    ReadRegStr $1 HKLM "${LEGACY_UNINST_KEY}" "UninstallString"
    ${IfNot} ${Errors}
        ExecWait '$1 /S'
        StrCpy $2 0
        ${Do}
            Sleep 500
            ClearErrors
            ReadRegStr $3 HKLM "${LEGACY_UNINST_KEY}" "UninstallString"
            ${If} ${Errors}
                ${Break}
            ${EndIf}
            IntOp $2 $2 + 1
        ${LoopUntil} $2 >= 120
    ${EndIf}

    # Both are ticked on a first install; later ones start from the last
    # choice, or from the one an installation for one user kept.
    ClearErrors
    ReadRegDWORD $0 SHCTX "${CHOICE_KEY}" "StartMenuShortcut"
    ${If} ${Errors}
        ClearErrors
        ReadRegDWORD $0 HKCU "${LEGACY_CHOICE_KEY}" "StartMenuShortcut"
    ${EndIf}
    ${IfNot} ${Errors}
    ${AndIf} $0 == 0
        !insertmacro UnselectSection ${SecStartMenu}
    ${EndIf}
    ClearErrors
    ReadRegDWORD $0 SHCTX "${CHOICE_KEY}" "DesktopShortcut"
    ${If} ${Errors}
        ClearErrors
        ReadRegDWORD $0 HKCU "${LEGACY_CHOICE_KEY}" "DesktopShortcut"
    ${EndIf}
    ${IfNot} ${Errors}
    ${AndIf} $0 == 0
        !insertmacro UnselectSection ${SecDesktop}
    ${EndIf}
FunctionEnd

Section "uninstall"
    !insertmacro wails.setShellContext
    SetRegView 64

    nsExec::ExecToLog 'schtasks /Delete /TN "${TASK_NAME}" /F'
    Pop $0
    RMDir /r "${DATA_DIR}"

    # The webview's cache of the person uninstalling. Their downloads,
    # settings and accounts stay.
    SetShellVarContext current
    RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"
    SetShellVarContext all

    # Only what the installer and the updates put there. A copy that is still
    # running goes at the next restart.
    Delete /REBOOTOK "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete /REBOOTOK "$INSTDIR\${PRODUCT_EXECUTABLE}.old"
    Delete "$INSTDIR\.${PRODUCT_EXECUTABLE}.update-*"

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    DeleteRegKey SHCTX "${CHOICE_KEY}"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols
    !insertmacro wails.deleteUninstaller
    RMDir /REBOOTOK $INSTDIR
SectionEnd
