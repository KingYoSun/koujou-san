# Issue #16: 固定成果物の独立検品

## 対象と入力

- 対象HEAD: `de779d2cc675b4b8462f3f6e9455c8eb72b5b76c`。基準main: `8d5499cfa72bf4bb44d1fd05646a9b19e1014f68`。
- 対象条件: [受入計画](2026-10-16-issue-16-plan.md)のScope revision 1、AC-1〜10の全末端とINVAR-1〜3。区分C。
- 検品者: 実装履歴を継承しない独立検品エージェント。実施日: 2026-10-17（JST）。
- 入力: Issue #16本文、設計書§6.4・7.1〜7.9・11.1〜11.2、ADR 0016、Runのrunbook、固定HEADの検証記録。実装者の会話・自己評価は受け取っていない。提供記録の成功記述は、独立実行とコードの到達経路で照合した。
- 確定済みの判断: 人のPause回答は、そのRunの編集権限を持つメンバーが行う。実行依頼の許可は要求しない。実CLI起動・Runner割当は対象外で、割当と構造化commandのproducerは専用fixtureに限る。
- 環境: Windows、PowerShell、リポジトリ同梱Go、pnpm、Rust、Dockerの専用PostgreSQL・Ory・ArcadeDB。既存8080/1420のプロセスは停止していない。実GitHub App、秘密鍵、一般利用者の認証ファイルにはアクセスしていない。
- 固定性: 開始時と独立試験後のHEADは一致し、作業treeはcleanだった。本記録以外の製品コード・既存テスト・Git状態は変更していない。追加試験はTempのGo overlayで行った。

## 条件ごとの結果

| 条件 | 確認した経路・到達結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | HTTP createでcommandのみを保存し、context/prompt・開始依頼を補完しない。欠落・空commandは400 | `TestRunsPostgres`、`validate` | PASS | なし |
| AC-2a | 固定Manifest・定義参照を使い、Contextと定義更新後も入力記録・Manifest本文が不変。作成時のSpecを独立したinputへ一度保存し、開始依頼や報告はその参照を維持 | PG試験162〜195行、`Create`/`Action`/`Report` | PASS | なし |
| AC-2b | 省略入力を補完しない。none Manifestからcontextを作らず、include/excludeを解決した固定本文に除外本文を入れない。現在認可を失った入力依存は読取り・export拒否 | PG試験、追加overlay、`Read`/`Create`/`manifestRefs` | PASS | なし |
| AC-3 | 一Runで二Pauseを順に受理し、回答受領→配送→再開確認を保存。解決済み二Pauseと開始inputを保持 | PG試験196〜307行、`Report`/`receive` | PASS | なし |
| AC-4a | 実行依頼許可のない二編集者の同時回答は一件のみ成功。同一operationの再送は同じreceipt・版へ収束。回答受領だけでは配送・再開にならない | PG試験214〜267行、`mutate`→`Store.Apply`の再認可・receipt・CAS | PASS | なし |
| AC-4b | 二回目待機中に一回目の回答を送ると409。解決後の再開重複も409。失効した編集者の再送は403 | PG試験256〜280行、Pause ID・応答ID・状態の照合 | PASS | なし |
| AC-5 | Pause前・再開後部分出力・失敗詳細・出力なし正常終了を区別。値・sequence・channel・source・成果物参照をOutputとEventへ保存し、GUIは読取り表示 | PG試験202〜351行、`Report`、`RunRecords` | PASS | なし |
| AC-6 | 再実行・command変更・修正の三Runは別IDと固定previousを持つ。関連を別レコードへ保存し、元の終端/input/outputを変更しない | PG試験316〜328行・export照合、`Create` | PASS | なし |
| AC-7 | CLI・人への依頼・Connector・独自commandが同じCreate/Reportを使用。派生名は任意。形式不正・欠落・不明判定をPASSへ変換しない | PG試験、`TestCommandAndInspectionContracts`、`assess` | PASS | なし |
| AC-8a | 終了コード0の検品FAILを実行succeeded・判定FAILとして保存し、表示でも区別 | PG試験282〜292行、RunStatus単体試験 | PASS | なし |
| AC-8b | cancel要求だけではrunningを維持し、担当端末のcancelled報告で取消確定。unknown報告は最後の実行状態を残してPauseを作らない | PG試験283〜286・370〜378行、RunStatus単体試験 | PASS | なし |
| AC-9a | 同一主体のHTTP/公式MCP SDKからRun/input/Pause/output/event/allを固定版・ページで比較し一致。Web/Tauriの追送画像・HTTP固定版・NDJSONを独立照合し、input・派生契約・固定成果物・回答者・原出力・状態が一致。両方のダウンロード実ファイルも証跡コピーと一致 | 独立`TestMCPIntegration`の`checkRunsMCP`、共通GUI/Native経路、後述の実操作証跡 | PASS | なし |
| AC-9b | 共通Accessの現在参加・資源・依存版・操作範囲を確認。none・非公開Run・他Workspace・権限外入力を拒否し、bounded操作は指定した一出力だけ取得。read操作のcreateを拒否し、参照/export前後でRun件数不変 | PG/実Ory-MCP試験、追加overlay、`ReadFilterSQL` | PASS | なし |
| AC-10a | 実ファイルへNDJSONを書き、保存記録と値を照合。二Pauseの全段階・部分出力・派生FAIL・三つの後続関連・unknown履歴・未取得成果物・固定Manifestを保持 | PG試験380〜425行・固定Manifest export、`exportHTTP` | PASS | なし |
| AC-10b | 権限外入力を持つexportは403。端末credential・generationを含めない。Pause中のexport後も同じRunで配送・再開・終了でき、元の固定記録も不変。export経路は接続先の変更を呼ばない | PG/実Ory-MCP試験、`exportHTTP`/`export_response`/`copy_export` | PASS | 実CLI/GitHubの継続は対象外。fixtureの継続を確認 |
| INVAR-1 | command必須。context/prompt/Issue/LLMを要求せずcommandのみで保存 | AC-1・AC-7の経路 | PASS | なし |
| INVAR-2 | 回答はPauseのID・応答者・由来と対応し、inputへの追記経路はない | AC-3・AC-4の保存/参照、`receive` | PASS | なし |
| INVAR-3 | HumanReviewもkind/targetを持つcommandで同じ入力検証と報告経路を通る | PG試験human、実Ory-MCPのHumanReviewRun | PASS | なし |

## 独立実行

| 検証 | 結果 |
| --- | --- |
| `pnpm run test:runs` | PASS。TestRunsPostgres 1.50秒、package 2.878秒。専用project `koujou-san-db-test-3e3ffca13514`のみ作成・破棄 |
| `pnpm run test:mcp` | PASS。TestMCPIntegration 59.51秒、package 61.586秒。実Ory・公式SDK・Run/HTTP比較を含む。専用project `koujou-san-db-test-333a03cf15d0`のみ作成・破棄 |
| `pnpm run test:contexts` | PASS。TestContextsPostgres 2.46秒、package 3.580秒。共通読取りの変更が固定版・none/exclude・Workspace・現在認可を維持。専用project `koujou-san-db-test-98fbc51cf4a2`のみ作成・破棄 |
| `pnpm run test:desktop` | PASS。5成功・環境依存5 ignored。新しいcopy_export試験は完了フレーム・固定版一致・途中終了・権限拒否を区別 |
| `pnpm exec vitest run src/runs.test.tsx`（apps/client） | PASS。1ファイル1試験。終了コード/業務FAIL/Unknown/取消要求/形式不正の表示を区別 |
| `go test ./internal/runs -run TestCommandAndInspectionContracts -count=1 -v` | PASS。入力binding・任意Context値・不正判定・外部schema参照拒否 |
| 検品用Go overlayで`pnpm run test:runs` | PASS。none/exclude、固定Manifest本文、一出力だけを許すbounded操作、範囲外の直接参照拒否、読取り目的でのcreate拒否、参照前後の件数不変。TestRunsPostgres 1.65秒、package 2.887秒 |

追加overlayは `C:\Users\kgm11\AppData\Local\Temp\koujou-issue16-review-dbf4d756da0e4221bc4fa60cbd29bd75\overlay.json` に保存した。最初の試行では検品fixtureの許可一覧にRun.input自身の固定参照を入れ忘れ、ページ要求がforbiddenになった。許可対象へinput参照を補って再実行し成功した。製品コードの変更や要件の緩和は行っていない。

CodeGraphの存在を確認し、既存の保存・認可・Context・MCP・Native入口を優先して調べた。今回の確認時点では新しいRunファイルも索引から取得できたため、そちらもCodeGraphで読んだ。索引の作成・更新はしていない。DESIGN.mdとADR 0014の欠落は入力条件として扱い、新しいUI要件は追加していない。

## Web/Tauri追送証跡の独立照合

最初の検品時はAC-9aを証跡待ちのINCONCLUSIVEとした。実操作担当から隔離fixture `716c2fb8b7b4`（HTTP 61884）の画像・HTTPスナップショット・実ダウンロードファイルが追送されたため、同じ固定HEADの差分検品として確認した。製品sourceには変更がなく、追送時の変更は証跡文書・画像・JSON・NDJSONだけだった。

| 対象 | Windows/Tauri | Web |
| --- | --- | --- |
| Runと回答後の固定版 | `run:ui-native-run` / `D4VG337QUYHQGS3JA2TW6I2NRW` | `run:ui-run` / `TVR243NBYHXKW7IU2R6KOAPZOL` |
| input固定版 | `SJ5GN2GYCCSEUHEOU7J4THGXQH` | `MP2IE3N7RZC6NCMLN7OIWF7MPY` |
| 回答後 | paused / known / 報告3、Pause received | 同左 |
| 回答者 | `L6JANPDNWTLCCMDTIBXNIJ7P6R` | 同左 |
| 配送・再開 | いずれも時刻なし。画像も未確認と表示 | 同左 |
| NDJSON | begin/completeのRun ID・版が一致、13記録 | 同左 |
| 実ファイルとコピーのSHA-256 | `351C4DFE00833F204AE680449E62975D395B16038E0886F2C3ABFB2237647671` | `EA7E6BE0F7DF1562EE02F86887B61B34F7686F7529312729B70EE37FDA077E3E` |

両方のJSON/NDJSONを独立したNodeスクリプトで読み、HTTPのhead・input・Pause・output（Nativeは回答前headも含む）とexport中の対応する記録全体を、キー順序を除いたJSON値で比較して一致した。inputはhuman command、派生契約HumanReviewRun、固定版付きunfetched成果物を保持する。原出力は `stdout` / sequence `2` / source `fixed-human-request` / 「Pause前の原出力」で、両画面の値とも一致した。回答は開始inputへ追記されず、Pauseへ回答者とともに保存され、回答受領だけで再開しない。

実操作時の主体は `can_execute=false` の管理者である。固定fixtureがGUI待機前に実行依頼許可をfalseへ戻す経路と照合した。編集権限による回答の成立は、別途独立PG試験の実行依頼許可を持たない二編集者でも確認済みである。

Nativeの[保存通知画像](assets/issue16-native-export.jpg)に表示された `D:\Library\Downloads\koujou-run-ecb44adc-281e-4ebf-93de-24a24c499ed3.ndjson` と、Webのdownloadイベントが返した `D:\Library\Downloads\koujou-run.ndjson` を直接読み、各assetsコピーとSHA-256が一致することを確認した。両ファイルともcompleteを持ち、credential・generation・access/refresh tokenの欄を含まなかった。秘密値の実ファイルを使って比較したものではない。

参照した証跡は [Native HTTP](assets/issue16-native-http-snapshot.json)、[Native NDJSON](assets/issue16-native-export.ndjson)、[Native input](assets/issue16-native-input.jpg)・[回答](assets/issue16-native-answer.jpg)・[原出力](assets/issue16-native-output.jpg)、[Web HTTP](assets/issue16-web-http-snapshot.json)、[Web NDJSON](assets/issue16-web-export.ndjson)、[Web回答](assets/issue16-web-answer.png)・[原出力](assets/issue16-web-output.png)。Webの[commandのみの画面](assets/issue16-web-command-only.png)と[HTTP](assets/issue16-web-command-only.json)も照合し、queued・報告0・開始依頼なし・起動時刻なし・context/promptなしを確認した。

実操作担当から、同fixture全体の `pnpm run test:mcp:ui` がPASS（427.52秒、package 429.743秒）し、専用サービスと所有する端末資格情報を片付けたとの結果を受け取った。このfixture終了操作は検品者自身の独立実行には含めない。HTTP/MCP本体は前述の独立試験でも成功している。

## 判定

- 総合判定: PASS。全末端AC・INVARを満たし、blocker・INCONCLUSIVEは0件。
- 確認した経路にblocker・合意済み挙動の回帰は見つからなかった。
- AC-9aのWeb/Tauri実操作証跡を固定HEADと照合して未確認を解消した。製品修正は不要。
- 最終head CI、merge、merge後照合、Issue Closeは本検品の判定対象外であり、親作業で別途行う。
- 本記録はyomiyasuの意味保持・情報不増補・主体と時制の確認に従って推敲した。
