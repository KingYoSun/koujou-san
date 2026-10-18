; Uses the RestartManager pattern from Tauri's NSIS utils (Apache-2.0 OR MIT):
; https://github.com/tauri-apps/tauri/blob/tauri-cli-v2.12.2/crates/tauri-bundler/src/bundle/windows/nsis/utils.nsh
; Unlike the app-window check, this guard never shuts down a running Runner.
!macro KOUJOU_REQUIRE_IDLE_RUNNER
  !define KOUJOU_GUARD_ID ${__LINE__}
  IfFileExists "$INSTDIR\koujou-san-runner.exe" 0 koujou_guard_done_${KOUJOU_GUARD_ID}
  !insertmacro RestartManager_StartSession $R0
  ${If} $R0 == ""
    SetErrorLevel 3
    Abort
  ${EndIf}
  !insertmacro RestartManager_RegisterFile $R0 "$INSTDIR\koujou-san-runner.exe"
  ${If} $0 = 0
    System::Call 'RSTRTMGR::RmGetList(p R0, *i .r1, *i .r2, p 0, *i .r3) i .r0'
    ${If} $0 = ${ERROR_MORE_DATA}
      !insertmacro RestartManager_EndSession $R0
      IfSilent +2
        MessageBox MB_OK|MB_ICONEXCLAMATION "Runnerが動作しています。新しい実行の受付を止め、実行中の処理が終わってから再実行してください。"
      SetErrorLevel 2
      Abort
    ${EndIf}
  ${EndIf}
  !insertmacro RestartManager_EndSession $R0
  koujou_guard_done_${KOUJOU_GUARD_ID}:
  !undef KOUJOU_GUARD_ID
!macroend

!macro NSIS_HOOK_PREINSTALL
  !insertmacro KOUJOU_REQUIRE_IDLE_RUNNER
!macroend
!macro NSIS_HOOK_PREUNINSTALL
  !insertmacro KOUJOU_REQUIRE_IDLE_RUNNER
!macroend
