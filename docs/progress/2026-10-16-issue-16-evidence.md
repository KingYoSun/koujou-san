# Issue #16: Run共通記録の検証

- 基準main: `8d5499cfa72bf4bb44d1fd05646a9b19e1014f68`。#15はPR #48の全CI・独立検品・マージ後照合後にClose済み。
- 対象: [計画](2026-10-16-issue-16-plan.md)のAC-1〜10・INVAR-1〜3、Scope revision 1。
- ユーザー判断: Pauseへ回答できるのはRunの編集権限を持つメンバー。実行依頼の許可とは分ける。
- 具体契約は[ADR 0016](../adr/0016-run-records-and-pause-protocol.md)、操作は[runbook](../runbooks/runs.md)。

## 条件と現在の観測

| 条件 | 観測経路と結果 |
| --- | --- |
| AC-1 | 専用PG/HTTPでcommandのみを作成。context/promptと開始依頼を補完しない。欠落・空commandは400 |
| AC-2 | 選択済みManifestと定義の固定版を使用。Contextと定義の更新後もRun入力参照・Manifest本文が不変。入力全文を各状態版へ複製しない |
| AC-3 | 固定adapterの構造化イベントで一Runの二Pause・回答・配送・再開を保存。解決済みも保持 |
| AC-4 | 実行依頼許可を持たない二編集者が同時回答し、一つだけ受理。同一再送は同じ版。古い質問・二重再開・役割失効後の再送を拒否 |
| AC-5 | Pause前・部分・失敗時の出力と、出力なし正常終了を区別。チャンネル・順序・出力元と符号化データ・未取得成果物を保持 |
| AC-6 | 再実行、command変更、検品後修正は別IDで固定版の関連元へ接続。元の終端・入力・出力は不変。後続の関係も別の固定記録 |
| AC-7 | CLI・Connector・人への依頼・独自の用途を同じ契約で保存。派生名は固定列挙にせず、必要な出力schemaと判定欄を宣言 |
| AC-8 | 終了コード0/実行Succeededと検品FAILを区別。取消要求だけではRunningを維持。切断はUnknownでPauseを捏造しない。外部イベントの応答は観測元と担当端末を記録し、人の回答と分ける |
| AC-9 | 実Ory・公式SDKでRun/input/Pause/output/派生契約の固定版・ページ・exportをHTTP/MCP比較。none・非公開Runを拒否。参照前後でRun記録数は不変。Web/Tauri実操作も後述の固定版・回答・原出力・ファイル保存が一致 |
| AC-10 | 固定版NDJSONを実ファイルへ保存し、記録を読み戻してID/版/順序/値/由来/状態を照合。Manifest固定内容・二Pause・三つの後続関連・未観測/未取得を保持。部分出力のあるPause中にexport後も同じRunを継続。端末資格情報は含めず、私有入力はexport拒否 |
| INVAR-1〜3 | command必須、context/prompt/Issue/LLMは任意。応答はPauseへ属し開始入力を変えない。HumanReviewもcommandを宣言 |

## 実行済みの検証

最新の `pnpm run test:runs` はPASS（1.43秒、package 2.560秒）。現在の担当端末の認証と許可を通したHTTP報告、二人の回答競合、固定入力、全出力種別、状態と判定、前後関連、ファイル読戻し、別Storeからの再取得を確認した。割当は本試験の所有データへ明示したfixtureであり、製品Runnerは実装していない。未確定sync-reportや自然文の推測を結果受理に使わない。

最終実装 `de779d2cc675b4b8462f3f6e9455c8eb72b5b76c` の実Ory/MCPとGUI fixtureはPASS（427.52秒、package 429.743秒）。隔離環境 `716c2fb8b7b4` で認証・HTTP/MCP・1万Context×3版・Web/Tauriを確認した。中断前の環境は30分で期限切れとなったため、再開時に環境を作り直して全体を完了した。

root check/test/build、desktop check/test/buildを実行した。rootのテストは11ファイル36試験。ネイティブは通常5試験PASS・環境依存5試験ignoredであり、live_context_ui_prepareは実操作環境で別途明示する。新しいストリームコピー試験は選択した固定版の完了フレーム、途中終了、版不一致、権限拒否を区別する。既存のPDB名衝突・linker警告はbuild失敗にはなっていない。

共通Context読取りのtransaction入口を追加し、既存のread/checkManifest/追加Context/操作範囲の経路を共用した。`pnpm run test:contexts` はPASS（3.02秒、package 4.457秒）。旧migration 001〜012は変更していない。

## 実装中の失敗と修正

HTTPのrecords取得とexportを同じ深さの可変pathに置いた初期案はServeMuxで競合した。execution操作のpathを分けて解消した。

最新Runの取得で、SELECT中の文字列化したpositionがORDER BYの対象になり、9から10へ進んだときに旧版を選んだ。実際の開始報告がstate_conflictとなる試験で再現し、tableのbigint列を明示して修正した。同じ試験が二Pauseと複数桁の反映位置を通過する。

export比較の初期assertionはJSONの空白差まで同一byte列として比較していたため失敗した。比較をUseNumberで数値を保持したJSON値へ修正し、原出力の文字列・チャンネル・順序と固定参照を保存記録と照合した。記録側をテストに合わせて変換してはいない。

CodeGraphを既存の保存・認可・Context・MCP・native入口の探索へ使用した。新規Runファイルは索引対象にまだ無かったため、その観測を確認して通常の検索・直接読取りへ切り替えた。索引は作成・更新していない。DESIGN.mdと参照されるADR 0014は存在せず、既存の共通画面のスタイルを使った。

## Web・Windowsの実操作

Computer UseとChrome拡張を使い、同じ最終実装へ接続した。Windowsアプリは公開されている対象タイトルバーをクリックして前面化し、その後の入力を実行できた。新しい恒久的な前面化修正を加えたわけではない。

| 観測 | Windows | Web |
| --- | --- | --- |
| Run | `run:ui-native-run` | `run:ui-run` |
| 回答後の固定版 | `D4VG337QUYHQGS3JA2TW6I2NRW` | `TVR243NBYHXKW7IU2R6KOAPZOL` |
| 固定input版 | `SJ5GN2GYCCSEUHEOU7J4THGXQH` | HTTP証跡内の固定参照 |
| 回答 | Windows受入: 確認して承認 | Web受入: 確認して承認 |
| 状態 | paused / known / 報告3、Pause received | 同左 |
| 配送・再開 | いずれも未確認 | 同左 |
| ファイル | DownloadsのUUID名NDJSON、完了通知 | ブラウザのdownloadイベントで実ファイルを取得 |

両方で実行依頼が不許可の管理者から回答した。回答者・時刻・受領状態をHTTPと照合し、元inputの参照が変わらないことを確認した。固定inputはhuman command、Context/promptなし、派生契約HumanReviewRun、固定版付き未取得成果物を表示する。原出力は `stdout`、sequence `2`、source `fixed-human-request`、本文「Pause前の原出力」が一致した。

実際のダウンロードファイルを読み戻し、begin/completeのRun ID・固定版、13記録、input・Pause回答・原出力のJSON値、未取得成果物をHTTP保存記録と比較してPASS。資格情報の欄を含まないことも確認した。開始済みCLIはこのfixtureには存在せず、実際の外部作業継続の代わりに、専用PG試験でPause中export後に同じRunを継続する経路を検証している。

WebではcommandだけのRun `run:2d86e80d-6a79-4b19-a1e4-2077da1f0c1b` も作成し、queued、開始依頼なし、起動未確認、Context/promptなしを画面とHTTPで照合した。画面の参照や回答からCLIは起動していない。

証跡は `assets/issue16-native-{input,answer,output,export}.jpg`、`assets/issue16-web-{answer,output,command-only}.png`、各 `*-http-snapshot.json`、`*-export.ndjson`、`issue16-web-command-only.json`。全て合成した検証用記録である。fixture終了後に専用サービスと端末内の所有資格情報を片付けた。

## 検品・CI・マージ

固定commitの独立検品は [記録](2026-10-17-issue-16-inspection.md) を参照する。実装headのCI run `38921863763` は全成功（Linux 5分7秒、Windows 7分40秒、GitGuardian）。証跡と検品記録を含む最終headのCI、マージ後tree照合はこれから確認する。現在の判定は [current-state](current-state.md) へ集約する。
