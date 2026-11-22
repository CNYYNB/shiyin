; 拾音 电脑版 安装程序（NSIS 3, Unicode）— 按用户安装，无需管理员权限
Unicode true
SetCompressor /SOLID lzma
ManifestDPIAware true
RequestExecutionLevel user

!define APPNAME "拾音"
!define EXE "Shiyin.exe"
!ifndef VERSION
  !define VERSION "0.2.5"
!endif
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Shiyin"
!define REGKEY "Software\Shiyin"
!define WV2URL "https://go.microsoft.com/fwlink/p/?LinkId=2124703"
!define WV2GUID "{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${APPNAME}"
Caption "${APPNAME} ${VERSION} 安装"
BrandingText "${APPNAME} ${VERSION}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\${APPNAME}"
InstallDirRegKey HKCU "${REGKEY}" "InstallDir"
ShowInstDetails nevershow
ShowUninstDetails nevershow

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=2052 "ProductName" "${APPNAME}"
VIAddVersionKey /LANG=2052 "CompanyName" "Louis"
VIAddVersionKey /LANG=2052 "FileDescription" "${APPNAME} 安装程序"
VIAddVersionKey /LANG=2052 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "© 2026 Louis"

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

!define MUI_ICON "..\winres\icon.ico"
!define MUI_UNICON "..\winres\icon.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "欢迎安装 ${APPNAME}"
!define MUI_WELCOMEPAGE_TEXT "拾音电脑版：播放电脑里的音乐，显示滚动歌词，并且可以通过 Wi-Fi 把手机拾音里的歌一键传到电脑。$\r$\n$\r$\n只为当前用户安装，不需要管理员权限。$\r$\n$\r$\n点击「下一步」继续。"
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "运行拾音"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

!macro CloseRunning
  ; 先礼貌地关闭窗口（让拾音保存播放进度），再强制结束
  nsExec::Exec '"$SYSDIR\taskkill.exe" /IM ${EXE}'
  Pop $0
  ${If} $0 == 0
    Sleep 1500
  ${EndIf}
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM ${EXE}'
  Pop $0
  Sleep 300
!macroend

Function HasWebView2
  ; 返回 $0 = 1 表示已安装 WebView2 运行库
  StrCpy $0 0
  SetRegView 64
  ReadRegStr $1 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WV2GUID}" "pv"
  ${If} $1 == ""
    ReadRegStr $1 HKLM "SOFTWARE\Microsoft\EdgeUpdate\Clients\${WV2GUID}" "pv"
  ${EndIf}
  ${If} $1 == ""
    ReadRegStr $1 HKCU "Software\Microsoft\EdgeUpdate\Clients\${WV2GUID}" "pv"
  ${EndIf}
  SetRegView lastused
  ${If} $1 != ""
  ${AndIf} $1 != "0.0.0.0"
    StrCpy $0 1
  ${EndIf}
FunctionEnd

Var Upgrade

Function .onInit
  ; 已经装过 = 升级（静默升级时会保留快捷方式和设置，并在完成后重新打开拾音）
  StrCpy $Upgrade 0
  ReadRegStr $1 HKCU "${REGKEY}" "InstallDir"
  ${If} $1 != ""
  ${AndIf} ${FileExists} "$1\${EXE}"
    StrCpy $Upgrade 1
  ${EndIf}
  ${If} ${Silent}
    Return
  ${EndIf}
  Call HasWebView2
  ${If} $0 == 0
    MessageBox MB_OKCANCEL|MB_ICONEXCLAMATION "这台电脑没有 Microsoft Edge WebView2 运行库，拾音需要它来显示窗口（Windows 10/11 一般自带）。$\r$\n$\r$\n点「确定」打开微软官方下载页面，安装后再打开拾音即可。$\r$\n（不安装也能继续，拾音会改用浏览器打开。）" IDCANCEL +2
    ExecShell "open" "${WV2URL}"
  ${EndIf}
FunctionEnd

Section "拾音" SecMain
  SectionIn RO
  !insertmacro CloseRunning
  SetOutPath "$INSTDIR"
  SetOverwrite on
  File "..\dist\${EXE}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; 快捷方式（开始菜单 + 桌面）；升级时如果用户删掉了桌面快捷方式，就不再加回来
  CreateShortCut "$SMPROGRAMS\${APPNAME}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\${EXE}" 0
  ${If} $Upgrade == 0
  ${OrIf} ${FileExists} "$DESKTOP\${APPNAME}.lnk"
    CreateShortCut "$DESKTOP\${APPNAME}.lnk" "$INSTDIR\${EXE}" "" "$INSTDIR\${EXE}" 0
  ${EndIf}

  ; 「应用和功能」里的卸载信息（当前用户）
  WriteRegStr HKCU "${REGKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APPNAME}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\${EXE},0"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "Louis"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" "$0"

  ; 拾音「内置更新」用 /S 静默升级：装完自动重新打开拾音
  ${If} ${Silent}
  ${AndIf} $Upgrade == 1
    Exec '"$INSTDIR\${EXE}"'
  ${EndIf}
SectionEnd

Section "Uninstall"
  !insertmacro CloseRunning
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${APPNAME}.lnk"
  Delete "$DESKTOP\${APPNAME}.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "${REGKEY}"
  IfSilent done
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "是否同时删除拾音的设置、歌单和封面数据？$\r$\n（你的音乐文件不会被删除）" IDNO done
  RMDir /r "$APPDATA\${APPNAME}"
  done:
SectionEnd
