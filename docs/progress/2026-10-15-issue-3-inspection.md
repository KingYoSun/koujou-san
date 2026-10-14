# Issue #3: 共通契約とnative試作の独立検品

## 対象と入力

- 関連: [Issue #3](https://github.com/KingYoSun/koujou-san/issues/3)、Scope revision 1、区分C。
- 基準HEAD: `f7ed4281555ee78c556c8a8b897aecaa4c591864`。ブランチ: `codex/issue-3-foundation-contracts`。
- 検品者: 別コンテキストの `issue3_independent_review`。実施日: 2026-10-15（JST）。
- 入力: IssueのAC/INVAR、設計書の対象節、ADR 0002、計画、実装・fixture・検証記録、合成probeの固定結果文の画像。
- 渡さない情報: 実装者の会話履歴、個人設定、CLI認証raw出力、実資格情報。契約と試作経路の確認に不要なため。
- 環境: Windows。コード・契約・保存/実行の経路と記録を照合し、Go/TypeScriptとnativeコンパイルの検証を独立実行。

初回は20ファイルのhashを開始・終了時に照合した。manifest digestは `03988F8619449C3ABE459C6B98EF9C5121E02EBA0106D94BC256428FC9E38DCC`。修正後は21ファイルを固定し、digestは `A1CDB4B0440ABFC360769CA1C56ACBB9644C576CE00ECEAADDB65D0B167389C5`。コード・fixture・設定17ファイルは初回と同じhashだった。

digestは、対象pathをPowerShellの `Sort-Object` 順に並べ、各行を `path + " " + 大文字SHA256` とし、末尾LFなしのLF区切りUTF-8をSHA256化した値である。後者はこの検品記録と、検品後のcurrent-stateの判定更新を含まない。

## 条件と結果

| 条件 | 確認                                                                                | 判定                                         |
| ---- | ----------------------------------------------------------------------------------- | -------------------------------------------- |
| AC-1 | ADRの責務表とGo/TypeScript型、共有状態・端末情報・認可の分離                        | PASS                                         |
| AC-2 | Go stdoutからTypeScript decoderへの接続、再送・競合・不完全性、認可取消後の再送拒否 | PASS                                         |
| AC-3 | surface判定は能力付与でなく、共通UIにCLI実行・資格情報取得APIがない                 | PASS                                         |
| AC-4 | #29の重複所有を修正。#12・#13を含む担当表をIssue一覧と照合                          | 修正後PASS                                   |
| AC-5 | nativeのstate・PKCE・code再使用・保存・固定UI応答の経路と観測記録を照合             | INCONCLUSIVE。OS採用判断と対象の確認が未完了 |

初回のblockerはADRの `#26〜#32` に配布担当#29が含まれ、配布の行でも列挙された重複だった。既存の担当へ合わせて除外し、修正差分を再確認した。新しい受入条件は追加していない。

`pnpm run test`（GoとUIの12テスト）、`pnpm run check`、`pnpm run check:desktop`、`cargo check --locked --example auth_boundary_probe -p koujou-san-desktop`、`git diff --check` は成功。文書修正後は変更差分とhashを照合し、コードのテストを重ねて実行しなかった。

native実機フローとCLIのモデル応答は、独立検品で再実行していない。コードと実装者の観測記録の一致を確認した。Credential Manager正常保存、Linux/macOS、本番Ory接続は未確認であり、試作やCI成功から補完しない。

今回のAC-1〜4に残るblockerは0件。Issue全体の完了、CI、マージ後の照合はこの検品だけでは判定しない。現在の判定は[現在状態](current-state.md)を参照。

## 採用判断後の再検品

同日にユーザーがWindowsデスクトップ・WSL内CLIを初期対象に選び、WindowsのClaude Code認証を復旧した。独立検品者は判断の回答、変更文書4件、Windows ClaudeとWSL接続の試験要約を追加で照合した。実認証raw出力を読まず、モデル呼出しも再実行していない。

Windows Claudeの終了0・期待応答・エラーなし・tools無効、WSLのstdin/stdout一致・stderr一致・終了0と記録が一致した。WSL内Claudeのモデル応答、Linux/macOSデスクトップ、本番Ory、製品Runnerを成功へ補完していない。コード・fixture・設定17ファイルのhashは前回と同じだった。

追加文書の固定digestは `89566DB010EBA1321D0877892004BDB2B712412B6562096CC6E8E9A2586D6316`（上記と同じ方法）。この追記と検品後のcurrent-state更新は含まない。AC-1〜5、INVAR-1〜2の具体的blockerは0件。PR #35のcommit `a2a609bda36f78507170853446a7296cb982375b` の共通UI/server・Windows native CI成功も確認した。追加文書を含む最新commitのCIとマージ後確認は別に実施する。
