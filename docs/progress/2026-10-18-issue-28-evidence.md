# Issue #28の検証記録

状態: In progress。Scope revision 1。基準は`17def2aa914e0c31cf8a03360521e57864d060d0`。PR #62でCIと独立検品を進めている。実接続とマージは未実施。

## 確定した判断

外部優先度はGitHubラベルの双方向同期とし、人の外部変更を固定として取り込む。割当は採用待ち、同じ方針・尺度の優先度順、同順位は受付順。期限は7日前・24時間前・到来時、全件定期評価は初期無効。モデル上限は入力1 MiB・出力4096 token・120秒・2試行・Workspace同時2評価で変更可能とし、保存済み設定は維持する。

## 変更前と途中の検出

- 方針のモデル設定を共有保存入口へ渡すテストは、変更前に`invalid_automation`で失敗した。共通のモデル設定を保存できるようにして同じテストが成功した。
- `none`の空入力を不足扱いにしていたため、初期の所有モデル試験で要求0回・`input_waiting`を検出した。空のManifestを許容し、本文やContextを黙って補わずに評価するよう修正した。
- migrationの途中でContext履歴の列名を誤って`position`とし、所有DBで失敗した。実際の`stream_sequence`に修正し、同じ新規migrationを通した。診断用の一時コードは削除済み。
- 確認済みの手動値から外部だけが変わる場合まで競合にしていた。`issue28-external-manual-before.log`で再現し、最後に同期した判断との比較へ修正した。同じ条件の`issue28-external-manual-after.log`はPASS。
- 同時取得した一括要求が、先行要求の消費未確定を完了扱いにしていた。`issue28-batch-concurrency-before.log`で再現し、送信前の待機と試行回数の保護へ修正した。`issue28-batch-concurrency-after.log`はPASS。
- 独立検品で、一括停止後のWorker巡回が要求を復帰不能にすることと、旧`apply-past`が現在要求の索引を更新しないことを検出した。`issue28-review-legacy-before.log`で両方を再現した。停止は再開できる待機とし、旧APIも共通の受付処理へ接続した。応答後の停止では保存済み結果と試行数を維持し、再開時に再送しない。
- この修正の途中でSQLのNULLとJSONのnullの区別を誤り、試行数が増えない失敗を`issue28-review-fixes-after.log`で検出した。両方を未完了として扱い、`issue28-review-fixes-after2.log`で既存条件と追加した三条件がPASSした。29要求と29利用量を照合した。
- API起動時にTriageのStartが二度呼ばれていた。独立検品の指摘を受け、依存先の設定後に一度だけ起動するよう修正した。
- 選択Contextの全子孫取得後にしか配置上限が効かない点も検出した。読み込み上限を追加すると、除外後の入力が小さい既存選択も保留になり得る。既存の「入力配置数」から「取得する固定版数」へ意味を広げる変更は未承認のため、実装せず本人の判断を確認する。

## 取得済みの証拠

| 記録 | 確認した範囲 |
| --- | --- |
| `test-results/issue28-triage-sync-owned.log` | 既存Worker基盤、人/Hopper/外部の起点×自動反映/提案、手動固定、古い応答、形式不正の2試行、判断不能、期限3境界、lease再取得、17要求と17利用量、応答喪失後のラベル照合、外部手動固定、内外の手動競合がPASS |
| `test-results/issue28-batch-assignment-owned.log` | 新しいmigration、一括評価の再送収束・ページ継続・基準後に作ったIssueの除外、一覧のカーソル、Issueの採用待ちと依存先の完了待ち、直接Runの維持がPASS |
| `test-results/issue28-hopper-regression.log` | 共通化した送信・応答・利用量保存で、既存Hopperの所有PG/HTTP試験がPASS。その後の割当・起票接続の変更は最終回帰試験で再確認する |
| `test-results/issue28-unit-all.log` | 初期の共通化後のGo全パッケージ試験がPASS。後続差分は最終検証の対象 |
| `test-results/issue28-owned-final.log` | 選択Contextの最新版、除外/none、関係対象だけの変更、禁止分類、費用上限、同時要求の消費待ち、26要求と26利用量、変化のない照合の履歴不増加、未反映の外部失敗の再送がPASS |
| `test-results/issue28-live-entry-owned.log` | 実接続用入口を所有HTTPで検証。二要求、2500/2623 byte、2048 token上限・30秒・fallbackなし、通常入力と本文内の優先度指示の両方がP1。実providerは使っていない |
| `test-results/issue28-ui-fixture.log` | 実Ory/MCPの共通保存・一括評価・一覧・版・権限、1万Context×3版と禁止分類、共通画面の最初の一巡、全試験がPASS |
| `test-results/issue28-ui-final.log` | Triageへ絞った画面入口で実Ory/MCPの照合、更新版の一覧順と名称表示、共通フォームがPASS |
| `test-results/issue28-check-final.log`、`issue28-test-final.log` | 静的検査、Go全パッケージ、共通UI 18ファイル64テストがPASS |
| `test-results/issue28-runs-final.log`、`issue28-publications-final.log` | 既存Runと外部起票のPG回帰がPASS |
| `test-results/issue28-hopper-final.log`、`issue28-hopper-diagnostic.log` | 最初は途中のClaimがnilとなり後続も失敗した。取得失敗の診断を追加した再実行は全Hopper条件がPASS。最初の失敗を削除せず、検品で原因と再現性を確認する |
| `test-results/issue28-independent-hopper.log`、`issue28-independent-hopper-repeat.log` | 独立した全Hopper回帰と診断付きSemanticの有限6回がPASS。最初のClaim失敗は再現せず、原因未確定。追加診断は別のClaimにあったため、元の失敗原因を説明した証拠として扱わない |
| `test-results/issue28-independent-migration-upgrade.log` | 001〜022の旧Scope・Grouping・固定/採用値・Hopper設定・jobを投入して023へ移行。既存3テーブルの双方向EXCEPT一致、新規jobゼロ、旧受付日時維持がPASS |
| `test-results/issue28-review-fixes-after2.log`、`issue28-review-fixes-final.log` | 一括停止→Worker巡回→再開、応答後停止→期限/試行上限後の保存結果の採用、旧API経由の実評価がPASS。修正差分は再検品の対象 |

これらの合格は記録を取得した時点の範囲であり、最新差分の全条件合格を意味しない。[独立検品](2026-10-18-issue-28-inspection.md)の初回結果はblocker三件。二修正の差分再検品は`c0daf08`でPASS。Context取得上限の判断、実provider・実GitHub・必須CI・マージ後照合が残っている。

## 共通画面の観測

Chromeの所有環境で、手動P2を固定したまま推奨P1を保持、本文更新後の再評価、引用と不明点、料金版`0.0320 USD`の合成費用、固定入力、理由付き解除、期限の保存・再取得、二件の一ページずつの一括要求を確認した。料金は所有試験の架空単価で、実請求ではない。

証拠画像は`issue28-manual-fixed.png`、`issue28-deadline-unlock.png`、`issue28-batch-pages.png`、`issue28-priority-groups.png`。期限`2026-10-26T10:29`はDOMで再取得を確認した。画像だけで期限の値を判定せず、日時欄は通常のキー操作で入力を確定した。更新版では採用グループが未採用より先に表示され、Issue名と状態を見出しで確認した。

二つの所有UI環境は終了APIから正常終了し、作成したタブを閉じた。Windowsアプリの再起動・スリープは追加していない。

## 実接続と画面の境界

実providerへの追加要求とGitHubへの新たな変更はまだ行っていない。#27で許可された三要求は使い切っており、#28へ流用しない。実行前に合成入力・上限・対象を具体化して確認する。二要求のモデル試験と、一件の合成Issue・二つの一時ラベルの同期試験の入口・メタデータ設定を準備済み。承認済みの消費台帳はまだ作っていない。専用キーや秘密鍵を文書・ログ・Gitへ記録しない。

共通UIではブラウザで一巡し、新しいOS境界を変更しない限りTauriで同じ操作を重ねない。ユーザーが提示した入口から参照される`DESIGN.md`と`docs/adr/0014-uiux-dev-flow.md`は、このcheckoutには存在しなかった。既存のフォーム・履歴・権限表示を使い、指示書やデザイン文書を推測して新設しない。

初回commit `ea4f2eb`のCI（run `39087728605`）はShared UI and server・Windows native client・GitGuardianがすべて成功した。独立検品の修正後のHEADについては、別途CIを確認する。`issue28-review-check.log`の静的検査もPASS。

本人へ、Context取得固定版の初期上限、最大二要求の実モデル試験、一件のIssueと二つの一時ラベルのGitHub同期試験を質問済み。回答前には依存する実装・実接続を行わない。修正後CIはrun `39088269171`で進行中。
