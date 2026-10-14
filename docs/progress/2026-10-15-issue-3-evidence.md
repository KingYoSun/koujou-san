# Issue #3: 先行検証の結果

- 日付: 2026-10-15（JST）
- 現在の判定: [現在状態](current-state.md)を参照。
- 試験端末: Windows 11 Pro 10.0.26200、Ubuntu 22.04 on WSL

## 共通契約

Goが生成した9種類のfixtureをTypeScriptで読み、同じ更新・再送、版競合、操作ID衝突、権限拒否、部分取得・投影待ちを照合した。GoのテストとUIの12テスト、型検査、整形検査は成功した。実DBの保存・本番認証・Domain APIはまだ実装していない。

`pnpm run check`、`pnpm run test`、`pnpm run build`、`pnpm run check:desktop`、`pnpm run test:desktop`、native例の `cargo check --locked --example auth_boundary_probe -p koujou-san-desktop` は成功した。native側にはまだ製品の単体テストがなく、test:desktopはコンパイルと空のテスト集合の確認となる。

## CLI

| 対象                            | 確認結果                                                                                                             |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| WindowsのCodex CLI 0.141.0      | 現在のユーザー設定を読み取れず、設定を省略した場合も既定/指定モデルが利用できなかった                                |
| リポジトリ内のCodex CLI 0.162.1 | 既存設定、read-only sandbox、ephemeral、stdin入力、JSONL出力で固定応答KOUJOU_PROBE_OKを確認。command実行イベントなし |
| WindowsのClaude Code 1.0.85     | 固定入力のJSON応答は得たが、OAuth access token invalidの401によりモデル応答を得られなかった                          |
| WSLのClaude Code 2.1.185        | 固定入力のJSON応答でNot logged inを確認。ログイン復旧が必要                                                          |

Codex 0.162.1は `.tools/codex` に導入し、アプリ同梱CLIや個人設定を変更していない。既定モデルはユーザーの接続設定を使った。raw出力と失敗記録はGit管理外のtest-resultsに保存した。CLIの対話、復元、継続、停止、詳細な利用量の対応範囲は #18・#24 で実際の採用構成を検証する。

## TauriのWindows試作

Goの合成issuerとTauriのnative loopback callbackを別プロセスに置き、システムブラウザのChromeから認証要求を送った。実際のOryや個人アカウント・実トークンは使っていない。

| 対象               | 確認結果                                                                                                                         |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| native例の起動     | Common Controls v6のmanifest不足による起動失敗を再現し、example用manifestで修正                                                  |
| state不一致        | native callbackがHTTP 400を返し、資格情報を書き込まなかった                                                                      |
| PKCE S256          | Rustのverifier/challengeをGoのissuerが照合。正しい長さの誤verifierを拒否し、正しいverifierだけ受理                               |
| コード再使用       | 一回受理したcodeの再送をissuerが拒否                                                                                             |
| Credential Manager | 合成トークンの保存に0x80070008が返った。この環境での正常保存は未確認                                                             |
| DPAPI CurrentUser  | native側で合成トークンを暗号化して一時ファイルへ保存し、再読取・復号・一致を確認。暗号文が原文と異なり、試作ファイルも削除できた |
| UIへの公開         | ブラウザの応答には結果文だけを返し、bearer token・verifier・暗号化保存内容を渡さなかった                                         |

DPAPIは明示した試作modeであり、Credential Manager失敗時の本番自動fallbackにはしていない。どちらも同一Windowsユーザー内の別プロセスからの読取まで隔離する保証は置かない。#5で採用OS・保存領域・失効/復旧・Oryとの接続を改めて検証する。

probe終了時にWebViewのwindow class解放エラーが出た。DPAPI modeのprocess終了コードは0、要求・交換・保存の確認結果は全て成功だった。productのネイティブアプリは変更せず、この試作の結果を本番認証や全OS対応の成功とは扱わない。

再試験はrootから二つのターミナルで行う。最初に `.tools/go/bin/go.exe -C apps/server run ./cmd/auth-probe-issuer` を起動し、表示されたissuer_portを使って `cargo run --locked -p koujou-san-desktop --example auth_boundary_probe -- <issuer_port> dpapi-current-user` を実行する。native例が返すauthorize_urlをシステムブラウザで開くと、callback・交換・保存の結果がnative stdoutへ出る。結果を確認後、issuerをCtrl+Cで停止する。これは合成資格情報の試作であり、OryやCLIのアカウント設定を変更しない。

Windowsで確認したブラウザの固定結果文は、Git管理外の `test-results/issue3-native-probe.png` に保存した。CLI raw出力と同様、画像をリポジトリへ追加しない。

## 未確認と判断点

Ubuntu 22.04 on WSLではGTK 3.24.33、WebKitGTK 2.50.4、DISPLAY、Rust、Claude Codeの所在を確認した。Linux native callbackとSecret Service等の保存は未検証。macOSの実機・CI確認も未実施。

初期デスクトップ対応OSの範囲をユーザーへ提示した。合意した範囲と、その対象の追加試験が揃うまでは #3 AC-5を完了扱いしない。Claude Codeの認証復旧についても、既存のアカウント認証を勝手に差し替えず、ユーザーの環境選択を待つ。

## 参照

- [Codex CLIの公式コマンド資料](https://learn.chatgpt.com/docs/developer-commands?surface=cli)
- [Windows Credential Managerの書込み](https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credwritew)
- [Common Controls v6を必要とするTaskDialogIndirect](https://learn.microsoft.com/en-us/windows/win32/api/commctrl/nf-commctrl-taskdialogindirect)
