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
| AC-9 | 実Ory・公式SDKでRun/input/Pause/output/派生契約の固定版・ページ・exportをHTTP/MCP比較。none・非公開Runを拒否。参照前後でRun記録数は不変。Web/Tauri実操作は後述へ追記予定 |
| AC-10 | 固定版NDJSONを実ファイルへ保存し、記録を読み戻してID/版/順序/値/由来/状態を照合。Manifest固定内容・二Pause・三つの後続関連・未観測/未取得を保持。部分出力のあるPause中にexport後も同じRunを継続。端末資格情報は含めず、私有入力はexport拒否 |
| INVAR-1〜3 | command必須、context/prompt/Issue/LLMは任意。応答はPauseへ属し開始入力を変えない。HumanReviewもcommandを宣言 |

## 実行済みの検証

最新の `pnpm run test:runs` はPASS（1.43秒、package 2.560秒）。現在の担当端末の認証と許可を通したHTTP報告、二人の回答競合、固定入力、全出力種別、状態と判定、前後関連、ファイル読戻し、別Storeからの再取得を確認した。割当は本試験の所有データへ明示したfixtureであり、製品Runnerは実装していない。未確定sync-reportや自然文の推測を結果受理に使わない。

実Ory/MCPの先行実行はPASS（57.08秒、package 59.045秒）。公式SDKで二つのRunの入力・質問・出力・イベント・固定版のHTTP一致を確認した。その後に開始依頼の名称・関連記録・Manifest出力・応答の由来を確定したため、この先行実行だけで最終状態を成功扱いにせず、最後の実操作fixtureで再実行する。

root check/test/build、desktop check/test/buildを実行した。rootのテストは11ファイル36試験。ネイティブは通常5試験PASS・環境依存5試験ignoredであり、live_context_ui_prepareは実操作環境で別途明示する。新しいストリームコピー試験は選択した固定版の完了フレーム、途中終了、版不一致、権限拒否を区別する。既存のPDB名衝突・linker警告はbuild失敗にはなっていない。

共通Context読取りのtransaction入口を追加し、既存のread/checkManifest/追加Context/操作範囲の経路を共用した。`pnpm run test:contexts` はPASS（3.02秒、package 4.457秒）。旧migration 001〜012は変更していない。

## 実装中の失敗と修正

HTTPのrecords取得とexportを同じ深さの可変pathに置いた初期案はServeMuxで競合した。execution操作のpathを分けて解消した。

最新Runの取得で、SELECT中の文字列化したpositionがORDER BYの対象になり、9から10へ進んだときに旧版を選んだ。実際の開始報告がstate_conflictとなる試験で再現し、tableのbigint列を明示して修正した。同じ試験が二Pauseと複数桁の反映位置を通過する。

export比較の初期assertionはJSONの空白差まで同一byte列として比較していたため失敗した。比較をUseNumberで数値を保持したJSON値へ修正し、原出力の文字列・チャンネル・順序と固定参照を保存記録と照合した。記録側をテストに合わせて変換してはいない。

CodeGraphを既存の保存・認可・Context・MCP・native入口の探索へ使用した。新規Runファイルは索引対象にまだ無かったため、その観測を確認して通常の検索・直接読取りへ切り替えた。索引は作成・更新していない。DESIGN.mdと参照されるADR 0014は存在せず、既存の共通画面のスタイルを使った。

## 残る確認

Web/TauriからのRun取得・input/派生/原出力の確認、実行依頼許可なしの編集者による回答、固定版のファイル保存、固定commitの独立検品、最終head CI、マージ後照合は未完了。現時点の試験成功と、Issue全体の完了は区別する。
