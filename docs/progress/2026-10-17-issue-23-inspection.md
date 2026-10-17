# Issue #23: 作業と判断の独立検品

## 対象と入力

- 対象HEAD: `4487a0c4c717ee075964a875a35fea5ce23c47d2`。基準main: `a12eade021e8131ec88f0023203b52aad3b38858`。
- 対象branch: `codex/issue-23-work-and-decisions`。Scope revision 1、区分C。
- 検品者: 実装会話を継承しない独立エージェント。実施日: 2026-10-17。
- 入力: `AGENTS.md`、`AGENTS.local.md`、文書案内、開発手順、Issue手順、[固定計画](2026-10-17-issue-23-plan.md)のAC-1〜7・INVAR-1〜3、設計書§5.1〜5.3、[ADR 0023](../adr/0023-work-and-decisions.md)、[運用手順](../runbooks/work-and-decisions.md)、[実装側の証跡](2026-10-17-issue-23-evidence.md)、指定ログ・既存画像、対象コードと基準からの差分。
- 前提: ユーザーが選択したWorkspace内「作業と判断」への集約、従来のRun/工程の編集権限による回答、実行権限との分離を固定した。新しい実行プリミティブ、Hopper、費用、追加モデル・OSの確認は対象外。
- 実装会話と自己評価は判定入力に含めず、証跡中の主張はコード・試験結果・画像で照合した。本人の初見操作は代行しない。
- 環境: Windowsの対象checkout。コード探索はCodeGraphを先に使用した。共有中のブラウザ・認証・fixture制御・SourceDBへは操作せず、秘密鍵、実接続設定、`.cache/*private*.json`は読んでいない。

## 条件ごとの結果

| 条件 | 確認した経路・操作 | 証拠 | 判定 | 未確認事項・確認の限界 |
| --- | --- | --- | --- | --- |
| AC-1 | WorkspaceからRun/工程実行のページ一覧、選択詳細、固定成果物、質問、検品条件と証拠へ接続。目的・工程・待機理由・担当・次操作を表示する | `workbench.tsx:244,270,453,511,581`、`workbench/http.go:50`、独立PG試験、`issue23-answer-resumed.jpg`、`issue23-return-original-verdict.jpg` | PASS | GUI画像のbuild後に補正された工程名取得元は、最新SQLとPG試験で確認した。補正後のGUIを新たには操作していない |
| AC-2 | 正常進行、Pause、Unknown、FAIL、INCONCLUSIVE、未実行、未送信、古いキャッシュを実行・観測・判定・同期の別表示で照合 | `workbench.test.tsx:43`、`workbench.tsx:95`、`run-pause.tsx:272,313`、`issue23-test-final.log`、`issue23-offline-cache.jpg`、`issue23-answer-resumed.jpg` | PASS | Unknownなど全八状態を実機で再生成した判定ではない。状態別表示試験と既存画面観測を組み合わせた |
| AC-3 | 二人目の編集者がPauseへ回答。API受付後もPauseのまま、配送報告後もPauseのまま、継続報告で再開する。回答案は端末保存と明示送信を分離 | `run-pause.tsx:188,254,404`、`runs/service.go:473,635`、独立PG試験、`issue23-answer-{received,delivered,resumed}.jpg`、実装側の共通画面観測記録 | PASS | 検品者自身は二アカウントGUIを再操作していない。画像の表示範囲だけで各段階すべての時刻を読めるとは扱わず、PGの状態・時刻確認で補った。所有Python実プロセスのGo試験接続であり、製品のネイティブRunnerを新たに実機確認した結果ではない |
| AC-4 | 質問・背景・選択肢・影響・待つ工程・専門知識を構造化表示。現在の閲覧・編集権限を持つ回付先へ保存し、二人目が判断不能を回答 | `run-pause.tsx:83`、`workbench/service.go:132,181,198`、`workbench.test.tsx:112`、独立PG試験、`issue23-answer-resumed.jpg` | PASS | 指名で権限は増えない。回付は相手の閲覧済み・回答済みを意味しない |
| AC-5 | 固定HTML `build-v1`、確認操作、対象版、条件、理由・証拠付き回答フォームを用意したコードと既存準備画像を確認 | `mcpapi/workbench_ui_test.go:78,89`、`run-pause.tsx:284`、`issue23-human-review-ready.jpg`、本人観測待ちの記録 | INCONCLUSIVE | 背景を知らない本人が画面だけで対象・操作・判断点を理解し、証拠付き回答を完了した観測がない。AIの読解や回答で置き換えない |
| AC-6 | 固定成果物・条件・問題・期待結果を持つ差し戻しを保存。明示作成した修正Run・再検品Runを既存の固定リンクで追跡し、旧判定と入力の不変を確認 | `workbench/service.go:145,224`、`workbench.tsx:535,730`、`runs.tsx:287`、独立PG試験、`issue23-return-related-runs.jpg`、`issue23-return-original-verdict.jpg` | PASS | GUIで関連させた二Runは未実行。修正・再検品の実行完了と前後参照はPGの固定接続で確認した。実製品の修正作業や再ビルドの成功と混同しない |
| AC-7 | 閲覧者の読取りと書込み拒否、実行不可の編集者の回答、権限のない回付先の拒否を照合。WebはローカルCLI設定・直接起動を要求しない | `workbench.test.tsx:21`、独立PG試験、`workbench/service.go:135,164,198`、`runs.tsx:505,549,949`、実行不許可の編集者を示す既存画像 | PASS | 編集者が既存の権限で新Runを定義する操作と実行を区別した。ネイティブOS境界の追加確認は対象外 |
| INVAR-1 | 一覧は名前と状態から選択でき、質問・条件・成果物が技術情報より先にある。識別子と原文は展開可能 | `workbench.tsx:453,497,840`、`run-pause.tsx:92,149`、`issue23-answer-resumed.jpg` | PASS | 一般の技術識別子を読む能力を初期表示の前提にしていないことを確認した。初見の理解自体はAC-5に残る |
| INVAR-2 | 回付保存、未送信案、API受付、Runner配送、継続確認を独立表示。回付保存と回答受領は既存Run状態を更新しない | `workbench.tsx:391,602,626`、`run-pause.tsx:313,425`、独立PG試験、段階別画像 | PASS | 外部通知の到達や相手の既読を観測したとは判定していない |
| INVAR-3 | 閲覧は読取りAPIと主体別ローカル保存。補足記録は専用表のみへ保存し、入力・Manifestへの注入経路を持たない。新Runは明示作成のみ | `workbench.tsx:195,244,270,376`、`workbench/service.go:187,225`、`runs.tsx:287`、独立PG試験の入力byte比較 | PASS | PG試験は既存入力の不変を直接比較。Manifest本文への書込みが追加されていないことは差分・呼出し経路で確認した |

コードのpathは、特記のないクライアントファイルが`apps/client/src/`、サーバーファイルが`apps/server/internal/`を基準とする。画像・ログは`test-results/`配下。

## 認可・保存・件数境界

`workbench/service.go`は、Runまたは工程実行の固定参照を既存の読取り認可で確認し、保存前と保存transaction内で編集権限を照合する。transaction内の`SourcesTx`で固定依存と現在の読取り権限を確認し、回付先にも同じ依存と回答権限を確認する。Pause付き回付は現在のactive Pauseとの一致を求める。共通CAS/receiptを使い、再送で同一記録を重複させない。

一覧はRun/工程実行の現在版をページで返し、入力本文・Context・原ログを取得しない。既定50件、上限100件とカーソルがある。補足記録は所有対象・数値位置の索引付きページで読み、メンバー候補もページで追加する。非公開の回付は一覧の担当と自分向け絞込みから除外される。最新の工程名取得と非公開回付は独立PG試験でも成功した。1万Context×3版での既存GUI観測は利用したが、処理時間の保証や新たな負荷基準は追加していない。

基準からの差分では、migration 001〜018に変更はない。019は新規の補足記録表と索引で、今回確認した適用先は専用試験DBである。回答は既存Run/Pause API、Run関連は既存の`previous`とリンクを使う。`runs.tsx`から回答処理を抽出して両画面を切り替えており、別の回答保存・実行経路は増やしていない。

## 検証と指摘

- 独立実行: `pnpm run test:workbench`を一回実行し、exit 0。`TestWorkbenchPostgres`は1.01秒、packageは2.148秒。ログは`test-results/issue23-independent-workbench.log`。専用プロジェクト`koujou-san-db-test-946e6e86bcc1`だけを作成し、終了後に同プロジェクトのDB・volumeを削除した。
- 既存ログ照合: `issue23-check-final.log`は型検査・書式成功、`issue23-test-final.log`はGoとUI 14ファイル46件成功、`issue23-client-build.log`は共通UI build成功、`issue23-database.log`はmigration・保存・CAS・停止・復元成功、`issue23-workbench-final.log`は最新PG試験成功。
- `git diff --check`は基準から対象HEADまで成功。検品中にHEADの一致を再確認した。
- 初回UI suiteは`issue23-ui-initial-failure.log`でFAIL。試験接続の更新を維持する修正と段階別GUI観測は確認したが、再実行suite全体の終了結果は未確認。最初のFAILを成功で上書きしない。
- 合意条件未達または今回の回帰を、到達経路と具体的な影響で示せる実装上のblockerは見つからなかった。未知の入力・将来の用途を追加要件にはしていない。

## 総合判定

**INCONCLUSIVE**。AC-1〜4・6〜7とINVAR-1〜3は、上記の範囲と証拠でPASS。AC-5は本人観測待ちのためINCONCLUSIVE。確認済み範囲の実装上のblockerは0件だが、全条件達成・Ready・Completeとは判定しない。

本人の初見操作と証拠付き回答、再実行中のUI suite終了、最終HEADの必須CI、merge後照合は未完了である。本人観測を追加した時点でAC-5を照合する。成果物に修正が入る場合は対象commitを固定し直し、その差分と影響条件を再確認する。
