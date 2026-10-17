# Issue #23: 作業と判断の証跡

- 基準main: `a12eade021e8131ec88f0023203b52aad3b38858`。Scope revision 2、区分C。
- [計画](2026-10-17-issue-23-plan.md)のAC-1〜7・INVAR-1〜3。現在の判定は[current-state](current-state.md)を参照する。
- [独立検品](2026-10-17-issue-23-inspection.md)ではAC-5以外をPASSとし、実装上のblockerは0件。AC-5はユーザー承認により未確認のまま#33 AC-4へ移管した。最終head CIとマージ後照合は未完了。

## 実装と局所確認

Run・工程実行のメタデータ一覧、選択した作業の詳細、構造化した質問、既存の検品条件と証拠を一つの入口へ接続した。入力メタデータはContext・prompt本文を返さない。回付と理由付き差し戻しだけを新規migration 019へ保存し、既存001〜018は変更していない。回答とRunの関連は既存の処理へ集約した。

専用PGでは、小さいページ、同じreceipt、現在の回付先権限、非公開回付のメタデータ非表示、二人目によるINCONCLUSIVE回答、受付/配送/継続の別状態、修正・再検品Runの関連、元入力と旧判定の不変を確認した。共通画面では正常・Pause・Unknown・FAIL・INCONCLUSIVE・未実行・未送信・古いキャッシュを状態別試験で確認した。

`test-results/issue23-check-final.log`は静的検査と書式確認成功、`issue23-test-final.log`はGo単体とUI 14ファイル46件成功。`issue23-client-build.log`は共通UI build成功。`issue23-database.log`はmigration 019を含む保存・停止・復元成功（package 12.225秒）。APIも`.cache/issue23-api.exe`へbuildした。

`issue23-workbench-final.log`は専用PGの最終試験成功（試験1.05秒、package 2.207秒）。現在の工程名について、単なる種別名でなくRunの待つ工程と工程実行の進行nodeを返すことも照合した。子Taskは独立したshared_versionsを持たないため、索引を使ってその子記録の最新固定版を読むよう補正した。既存migrationや子記録の形式は変更していない。

## 共通画面と実CLI

実Ory、専用PG、1万Context×3版の所有fixtureを使った。Windowsアプリの起動・認証・終了・スリープは再試験していない。二つのCLIプロセスは既存の所有Python fixtureであり、Goの試験用接続が既存Run APIへ報告する。製品のネイティブRunnerや物理端末を新たに検証したとは扱わない。

初回は試験用接続の定期更新が不足し、質問待ちの間に割当とRunnerが失効して、製品が報告を拒否した。初回suiteはFAILで、ログは`test-results/issue23-ui-initial-failure.log`。試験用接続へ20秒ごとの通常の接続更新を入れ、同じ条件を再実行した。製品の認可を緩和していない。

更新後の環境はAlice `http://127.0.0.1:52131`、Bob `http://127.0.0.1:52132`。いずれも所有fixtureの一時URLで、製品の公開URLではない。実行担当から別メンバーへ専門判断を回付し、実行不許可の編集者がINCONCLUSIVEを回答した。API受付後も配送・継続は未確認のままで、fixtureの明示操作によって配送確認、実際のCLIの継続報告、正常終了とINCONCLUSIVEの別表示へ進んだ。

このUI環境のbuild後、一覧SQLの非公開回付の表示判定と工程名の取得元を補正した。上記の専用PG試験で追加分を確認した。共通UIが読むフィールドと、質問・回答・回付・差し戻しの操作契約は同じで、本人観測用の質問画面も変更していない。

- `test-results/issue23-answer-received.jpg`: API受付、配送/継続未確認。
- `issue23-answer-delivered.jpg`: Runner配送確認、継続未確認。
- `issue23-answer-resumed.jpg`: 継続確認、正常終了とINCONCLUSIVE。
- `issue23-offline-cache.jpg`: 初回環境でAPIを切断し、古い表示と未送信回答案を区別。復帰・再読み込み後も回答案が復元され、自動送信されなかった。試験用接続の失効修正はこの表示・保存経路を変えていない。

FAILの検品から当時の条件・Stale・期限切れ証拠を取得し、固定成果物・条件・問題・期待結果を持つ差し戻しをGUIで保存した。既存の明示作成で修正Runと再検品Runを作り、前後のリンクを辿って元のFAILと差し戻しへ戻れた。GUIで作成した二Runは未実行として表示し、起動を依頼していない。修正・再検品の実行状態は専用PGの固定接続で確認する。

- 修正: `run:92b78d67-9a82-447f-9e3a-9357813118bd`。
- 再検品: `run:aa75c0bf-fa0f-41b2-9c07-193577f7a316`。
- `test-results/issue23-return-related-runs.jpg`、`issue23-return-original-verdict.jpg`: 関連と元判定。

## 本人観測の準備と#33への移管

AC-5の依頼は「受入: 固定ビルドの画面を確認」。対象は所有する静的HTMLの`build-v1`、質問に目的・確認操作・対象版・条件・選択肢の影響を表示する。背景を説明せず、画面内の情報から成果物を開き、判断・理由・証拠URLを返す操作を本人へ依頼した。準備画像は`test-results/issue23-human-review-ready.jpg`。AIが本人の代わりに判断したとは記録せず、観測結果が揃うまでAC-5を成功にしない。

実装成果物は`4487a0c4c717ee075964a875a35fea5ce23c47d2`、PRは[#57](https://github.com/KingYoSun/koujou-san/pull/57)。独立検品者は同commitと基準mainの差分を固定し、専用PG試験を一回実行して成功した（試験1.01秒、package 2.148秒）。ログは`test-results/issue23-independent-workbench.log`。AC-1〜4・6〜7とINVAR-1〜3はPASS、AC-5は本人観測待ちINCONCLUSIVEであり、全条件達成とは扱わない。

2026-10-17、ユーザーはこの一回の初見操作を#33のリリース前受入へ移し、未確認と記録して先へ進む方針を選択した。#23はScope revision 2としてAC-5を完了条件から外し、#33 AC-4が同じ対象・操作・証拠付き回答の確認を引き継ぐ。人の理解を確認したとは扱わず、#33で未実施ならリリース受入を完了しない。

再実行したUI suiteは本人観測待ちのまま30分の有効期限に達し、`UI inspection timed out`でFAIL終了した（1902.67秒、ログ`.cache/issue23-ui-run.log`、保存先`test-results/issue23-ui-timeout.log`）。製品の回答処理の失敗ではないが、suite全体を成功へ読み替えない。既に取得した段階別GUI観測、専用PG、状態別UI試験は上記の証拠として保持する。専用サービス・volumeは試験が終了処理で削除し、秘密値入りfixture設定も削除した。初回fixtureのFAILも失敗記録として残す。

記録更新前のhead `a8fdd60e67bda147a588e9073d7f079cc2532b86`はCI [39020954917](https://github.com/KingYoSun/koujou-san/actions/runs/39020954917)のShared UI and server・Windows native client、およびGitGuardianが全成功。記録更新後の最終head CIとマージ後照合を確認して完了を判定する。
