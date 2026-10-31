# Issue #82の検証記録

対象は[Issue #82](https://github.com/KingYoSun/koujou-san/issues/82)、Scope revision 1。基準は`6867594722295a3e726d642857ab7380d4d8660e`、実装は`7debc41`、実操作と初回独立検品による修正は`1303be5`。現在の判定は[実装計画](2026-10-31-issue-82-plan.md)へ集約する。

## 環境と確認範囲

Windowsの共通Web / Tauri UI、専用PostgreSQL、実Ory、Bleve、ArcadeDB、製品の独立Runnerを使用した。Contextは既存の1万件×3版の試験を維持した。ユーザー指定のモデルはChatGPTサブスクの`gpt-6.1-sol`、一巡の対象はkoujou-sanのIssue #83である。

#83については、GiteaとGitHubの識別、既存GitHub機能の維持、未確認範囲と次の入力をまとめる小さな準備文書を成果物とした。Gitea本実装、実Gitea接続、CI・LFS、公開・配備は行っていない。#82の実装完了と#83の完了を同一視しない。

## T1: 相談と共有保存

WindowsアプリでProjectを選び、Issue・Runなしでスレッドと人間の発言を保存した。本人が今回の接続用OAuth認証を完了した後、既存のモデル接続で一つの選択メッセージと今回の依頼を送信した。

[実応答receipt](assets/issue82-model-receipt.json)の要求モデル・実モデルはいずれも`gpt-6.1-sol`、HTTP 200、prompt 254 / completion 279 token。単価未設定のため金額は未算定である。会話の費用はclient operationへ帰属し、Runの利用量へ複製しない。[Windowsの採用画面](assets/issue82-native-adoption.png)を保存した。資格情報は通常の暗号化保存を使い、証跡へ含めていない。

共有保存の専用DB試験では、人間・AI・関連作業の区別、要求IDによる再送照合、固定版、異なる二利用者、Scope送信禁止、サービス再生成後の同じ履歴を確認した。API・Host試験は完了、不完全応答、失敗、送信結果不明を区別する。応答受付を失った場合も、再表示だけでモデルへ再送しない。

11月1日の補完では、別の管理下HTTP環境で送信しない履歴と送信する履歴を別々に保存し、後者だけを選択した。[実HTTP要求](assets/issue82-controlled-http.json)は一件で、選んだ固定履歴と今回の依頼のみを含む。除外履歴、別スレッドの60件、Context本文は含まれず、応答完了後の再読込でも追加要求はなかった。

## T2: 仕様採用から固定入力へ

管理下HTTPの会話から採用したContextは`chat-2b460900dc9e796288740bff2add949cfe7767fc25bde2c92ce5eee7e811baef@A54EGICXLSCCJDN2KA2JU4PK2I`。元メッセージの版、digest、UTF-8範囲を由来に持ち、Projectへの所属を保存した。

「この仕様でRun定義を準備」から既存の工程画面へ進み、採用Contextを固定した下書きを生成・編集・検証・採用した。保存しただけではRunを開始せず、入力確認、Run作成、開始依頼、端末割当をそれぞれ実操作した。[固定入力の画面](assets/issue82-web-fixed-input.png)を保存した。

[一巡のJSON](assets/issue82-cycle.json)は次を照合している。

- 保存Contextの固定版と、定義`run-definition:5476fb9f-6b97-4094-ac3b-b5db09b9910f@YSEXNUU3KPSWJKSY6PRI7Y7KR3`の選択。
- 同じ選択を既存Context APIで解決したManifestのplacementsと本文。digestは`c58d7e66432bc45e543add84986bce9ade27b49c3d2bf7bfc2b99c29e388ba35`。
- `run-input:issue82-preparation-correct-input@B6M27YLRTFQNBGVQVEY5EV3KCQ`のContextと、CLIが受け取ったJSON。いずれも採用したSentence一件のみ。

既存の単独Run作成経路は、選択を解決して本文と出典を固定入力へ保存する。別の永続Manifestを追加する変更は行わず、上記は同じ選択のプレビューとの照合である。会話全文、前Runの出力、実装者の評価は入力へ追加していない。

## T3: 質問回答、実行、独立検品

対話接続は検証用の`jsonl-v1` CLIで、明示的なPause frameを出す。自然文を質問と推測せず、非対話のCodex接続にはPause能力を宣言していない。このCLIは通常のRunnerから固定入力を受け、回答が`preparation`の場合だけChatGPT認証済みCodexの`gpt-6.1-sol`へ本文生成を依頼し、専用worktreeに準備文書だけを保存する。

共通画面の既存回答部品で選択・下書き保存・再訪・送信を行い、Pauseのwaiting / received / delivered / resolvedを共有記録と照合した。[狭い画面の質問](assets/issue82-web-narrow-question.png)、[API受付](assets/issue82-web-answer-received.png)を保存した。受付だけで実行再開と表示せず、再開報告を取得してから区別する。

| 記録 | 観測結果 |
| --- | --- |
| `run:issue82-preparation-correct-input@74OT5BTTJPGEXUYFXBV7VP6VAP` | 14:04:47〜14:06:17 UTCに実行し正常終了。固定成果物はcommit `7fa7fb50a71404e5b714b330a7b30803ada777ea`の`docs/progress/2026-10-31-issue-83-chat-proof.md` |
| Git本文のSHA256 | `a1d5a5bfcc2041f4fbb295e95be1519b51841ccf4d14eb3b85374562e7ea2cf2` |
| `run:issue82-independent-inspection@KIM4KSSR2DQUHHJZ7FU5EYHIWD` | 別のCodex呼出し。Git読取りが実行方針に拒否され、正常終了だが検品はINCONCLUSIVE。この判定を保持 |
| `run:issue82-snapshot-inspection@HIMGWLJYX4IF5OBRFNLZGNJJGI` | 同じGit本文を既存snapshot形式で固定し、別のCodex呼出しで4条件を検品。形式valid、4条件PASS、未確認項目なし |

snapshotのJSON digestは`68fc523c10964fba012f3b3b06d55edcc6b9d8c3ab0bc99bbd8e87b73cad4258`。本文そのもののdigestと区別してAPIで検証した。検品入力はContext `none`、固定成果物、採用済み4条件のみ。実装会話、前Runの生出力、実装者の自己評価は渡していない。検品は`--ephemeral`を使う別CLI呼出し・別worktreeで行った。製品が観測していないsession / workspace / permissions / external_accessの分離強度は`unobserved`のままである。

[条件別の検品原記録](assets/issue82-web-inspection-criteria.png)と[会話の結果表示](assets/issue82-web-chat-results.png)で、正常終了・INCONCLUSIVE・PASSを別に表示し、原記録へ移動して元スレッドへ戻れた。既存の工程・CI等の状態も同じ業務サービスの記録へ接続する。今回の#83文書にはCI実行結果は発生していない。

CI表示は別の管理下HTTPスナップショットで補完した。既存WorkflowのCI観測がGET一回・書込み0回で結果を保存し、スレッドの関連工程に`ci（1回目）：合格（PASS）`を表示した。リンク先の原報告に`source: github-observation`、固定commit、Run 77、required jobのcompleted / successがあることを確認した。[元記録](assets/issue82-ci.json)、[スレッド画面](assets/issue82-web-ci-thread.png)、[原報告画面](assets/issue82-web-ci-source.png)に保存した。これは合成CI観測の表示試験であり、外部CI実行や#83のCI成功ではない。

## T4: 再訪とGUIを閉じた後の継続

[履歴の観測](assets/issue82-web-history.json)では、60件の会話から最新50件（10〜59）を取得し、「以前の会話を取得」で前の10件（00〜09）を取得した。閲覧によって選択送信件数は増えない。

`run:issue82-gui-continuation`を開始し、質問待ちのまま画面を閉じた。閉鎖は14:27:55.666〜14:27:55.676 UTC。その前14:27:50.042と後14:28:04.724のRunは、同じ版`AFNBQ3M4PRV6ZO3SNZ67YRRZJ7`、同じ開始時刻、同じactive Pauseであった。既存の独立Runnerが処理を保持していた。再訪して`hold`を回答すると、モデルを呼ばずに14:31:21.147 UTCに正常終了した。同じ依頼の追加起動はない。

390px幅で横スクロールなしに質問・回答欄を確認し、Tab / Shift+Tab / Enterと関連先からの戻りを実操作した。試験後はviewportを元へ戻した。CSS zoomによる試行はブラウザー200%拡大と同等ではないため、拡大表示の合格証拠には含めない。

[キーボード・再訪の観測JSON](assets/issue82-ui-observations.json)には、Tabで次のボタンへ移動、Shift+TabでCIリンクへ戻る、Enterで固定Runへ移動、元対象リンクで同じスレッドへ戻る操作を記録した。再読込後もスレッドのID・版・題名が一致した。

## 修正前の失敗と確認した修正

最初に追加した選択履歴のテストは実装前に`invalid_client_operation`で失敗し、実装後に成功した。初回の独立ソース検品では、Workspace編集者かつProject閲覧専用の利用者が、AI応答の保存経路を通れるP1を確認した。Start / Begin / Finishで現在のProject更新権限を再確認し、開始前・送信前・結果保存前の失効を含む回帰試験を追加した。修正前FAIL、修正後PASSを確認した。

実操作では、既存定義生成部品の選択が空で初期化されていたため、定義本文の選択を共通editorStoreへ反映するよう修正した。回答後にRunのactive Pauseが消えた際も、直前の質問を再取得して配送・再開を確認できるようにした。

検証環境では次の失敗も発生した。製品の成功証拠として数えていない。

- 最初の30分制限のTLS環境はRun開始前に期限切れとなった。実Sol応答の確認に使い、実Run一巡は別の専用環境で実施した。
- 大量Context準備中に一度`storage_unavailable`で停止した。再実行では既存の大量件数条件を変えず成功した。
- 検証用Runnerの深い保存先で準備が停止した。自分の専用保存先を短くして同じ未開始Runを再開した。開始済みRunの自動再送はしていない。
- 最初の検証用CLIはContextオブジェクトを`[object Object]`として渡し、不十分な文書を出した。`run:9e785e4a-0db6-4080-8b52-d7360be960e4`の結果を保持し、CLIのJSON直列化を修正して、別の明示した依頼を作った。
- 最初の独立検品はGit読取りを拒否されINCONCLUSIVEとなった。同じ固定本文のsnapshotを使う既存経路で再検品し、実行方針は緩和していない。

## 実行した検証

| 検証 | 結果 |
| --- | --- |
| 専用DBの`pnpm run test:clientops` | PASS。会話、固定採用、再送、二利用者、Project更新権限、送信禁止Scope、usage、既存選択・定義生成を含む |
| 共通UI `vitest run` | 29ファイル・107テストPASS |
| 採用版Go 1.27.3の`go vet ./...`、変更Goファイルのgofmt、`git diff --check` | PASS |
| rootの`pnpm run build` | PASS。GoとWebの型検査・build |
| Windowsの`pnpm run build:desktop` | PASS。ネイティブ固有コードの変更なし |
| `pnpm run test:chat:ui` | PASS。実Ory・1万Context×3版・実UI・既存MCP停止/期限切れ拒否を含み、終了後に専用サービスを削除 |
| rootの`pnpm run check` | 既存のignored `apps/server/.cache/issue33-config/main.go`の書式で停止。変更対象の書式・Go vet・型検査を個別確認。CIはclean checkoutで別途確認する |

このPCのstandalone `pnpm.exe`経由では、既存スクリプトがexeをNodeで読もうとして失敗した。同じ10.16.1の`C:/Users/kgm11/AppData/Roaming/npm/pnpm.cmd`でbuildを実行した。直接PATHの古いGoも不適合だったため、規定の`.tools/go`を使った。誤った`@koujou/client`指定では対象がなかったので、正式名`@koujou-san/client`で107テストの成功を確認した。

検証用Runnerはactive 0、未送信frame・usage・不明実行なしを[確認](assets/issue82-runner-before-stop.json)して通常停止した。専用Docker環境だけを終了し、既存の別プロジェクトのサービスは操作していない。
