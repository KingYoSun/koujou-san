# Issue #31: 固定出力からのContext生成

状態: In progress。条件は[計画](2026-10-18-issue-31-plan.md)のAC-1〜7・INVAR-1〜3。契約は[ADR 0031](../adr/0031-output-context-generation.md)。

## 変更前後と局所検証

- `test-results/issue31-generation-before.log`: 通常の所有Runを作成・報告して固定outputを得た後、既存版はgeneration方針を保存できずFAIL。
- `issue31-generation-start-after.log`: 固定出力の生成操作を、補助RunやPauseなしで開始できた。実PGでPASS。
- `issue31-revision1.log`: 指定箇所から四粒度を作成し、固定由来・子参照・二モデル・既存新版・固定値・所属・操作usageを同じ実PGで確認。生成によるRun記録/Pauseの増減なし。PASS。
- `issue31-auto1.log`: 方針保存/有効化で過去を処理せず、新しい確定出力だけを担当本人/クライアントへ渡す。受領応答の再取得で同じ操作を返し、完了したsourceを重複生成しない。PASS。
- `issue31-backfill1.log`: 過去の出力は明示した期間の依頼で照合し、処理済みの同じ方針版/outputを再生成しない。手動修正の通常採用と版単位の由来取得もPASS。
- `issue31-generation-host.log`: 共通Host/JSON精度の12試験はPASS。noneのContext生成ではrun_getだけを渡し、未送信の固定値と既存Context IDを初期メッセージへ含めない。
- `issue31-ui-local.log`: 既存Context編集・Host・精度保持の13試験、共通UIの型検査はPASS。

実PGのモデル応答とMCP監査は所有fixtureである。実Ory/MCPと共有画面は検証中。実モデルの新しい要求・認証は行っていない。native搬送は変更せず既存証拠を再利用する。独立検品・必須CI・merge後照合は未実施。

## 固定出力・共通画面と障害分岐

- `issue31-baseline-conflict.log`: 生成後も過去Manifestと開始済みRun入力はbyte単位で不変。新Contextは既存基準版では選べず、明示採用した基準版でのみ選べる。他編集者との競合で先行した新規作成もrollbackする。PASS。
- `issue31-mcp1.log`: 実Ory・SDK経由で指定出力箇所だけを取得。別チャンネル・元入力を返さず、noneとScope範囲から外れるContext本文・Run一覧・編集を拒否。共通MCPと大量件数の既存試験もPASS。
- `issue31-context-ui.json` / `issue31-ui1.log`: Chromeと二つの所有モデルHTTPで実操作。原出力から生成画面へ進み、四粒度と所属を自動採用、通常のContext画面で手動編集し、変更後の由来表示を確認した。モデルβで既存の同じ記述を更新し、モデルαの案から既存記述だけを明示採用した。方針の保存、有効化、新しい固定出力の自動処理、無効化も確認した。元出力は二件、生成待ちは一件。受信側でOry cookie・MCP認証header・指定外本文が無いことを検証した。
- 画面で発見した連続生成の競合は、結果版の引継ぎと新規Context IDの操作単位化で修正した。後者は画面試験の旧serverでは未反映で、`issue31-core-final.log` の同じ結果へ二回生成する試験で修正後PASSを確認した。
- 方針有効化では、HTTP応答のworkspace_idを固定参照DTOへ混ぜていた不具合を修正し、同じ画面で保存済み方針の有効化から自動反映までPASSを確認した。
- `issue31-failure-recovery.log`: モデル503、累積要求上限、送信途中の担当クライアント終了を発生させた。原応答・送信意図を保持して重複送信を拒否し、失敗・上限・結果不明からモデル要求なしの手動案を採用できた。PASS。

新しい実モデルの要求・認証とWindows実機の操作は行っていない。共通Webで変更箇所を確認し、native搬送は既存の証拠を再利用する。

## 候補の局所検証

`issue31-check-final.log` はGo vet・共通UIの型検査・全対象のformat検査PASS。`issue31-test-final.log` はGoの全packageとUI 18ファイル68試験PASS。`issue31-failure-recovery.log` は最新のclientops統合試験PASS。配布更新・復元の現在schema番号24はPRの共有CIで確認する。

| 条件 | 証拠と判定 |
| --- | --- |
| AC-1 | 四粒度・所属・固定由来・原出力不変を実PGと共通画面でPASS |
| AC-2 | 既存新版・構成参照・所属・通常手動編集と明示基準版の選択をPASS |
| AC-3 | 四状態・自己申告の分離・候補の非削除・通常編集権限・過去判定不変をコード経路と画面でPASS |
| AC-4 | 二つの所有モデル・同じ共有記述・手動/提案/自動・固定値・他編集者の競合・原応答保持をPASS |
| AC-5 | 通知と応答の再送・同一結果への連続生成・担当終了・503・累積上限・手動復旧をPASS |
| AC-6 | Run記録とPauseが同数、操作のみのusageを照合してPASS。生成経路にcommand/Workflow作成なし |
| AC-7 | 過去input/Manifestのbyte不変、基準版未採用では選択不可、明示採用後は選択可能をPASS |
| INVAR-1〜3 | 文書/所属/注入の分離・原記録保持・実MCPの現在権限と指定箇所への制限をPASS |

これは実装側の判定であり、固定成果物の独立検品、CI、merge後照合はこれから行う。
