# Issue #28の検証記録

状態: In progress。Scope revision 2。基準は`17def2aa914e0c31cf8a03360521e57864d060d0`。実OpenAI二要求・実GitHub・独立検品はPASS。PR #62の最終文書を含むHEADのCIとマージ後照合が残っている。

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
- 選択Contextの全子孫取得後にしか配置上限が効かない点も検出した。読み込み上限を追加すると、除外後の入力が小さい既存選択も保留になり得る。既存の「入力配置数」と異なる制限のため本人の判断を確認し、Scope revision 2で承認後に実装した。

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

これらの合格は記録を取得した時点の範囲であり、最新差分の全条件合格を意味しない。[独立検品](2026-10-18-issue-28-inspection.md)の初回結果はblocker三件。二修正の差分再検品は`c0daf08`でPASS。取得上限は`37659e4`の差分検品でPASS。実接続の最終結果と残作業は末尾へ集約する。

## 共通画面の観測

Chromeの所有環境で、手動P2を固定したまま推奨P1を保持、本文更新後の再評価、引用と不明点、料金版`0.0320 USD`の合成費用、固定入力、理由付き解除、期限の保存・再取得、二件の一ページずつの一括要求を確認した。料金は所有試験の架空単価で、実請求ではない。

証拠画像は`issue28-manual-fixed.png`、`issue28-deadline-unlock.png`、`issue28-batch-pages.png`、`issue28-priority-groups.png`。期限`2026-10-26T10:29`はDOMで再取得を確認した。画像だけで期限の値を判定せず、日時欄は通常のキー操作で入力を確定した。更新版では採用グループが未採用より先に表示され、Issue名と状態を見出しで確認した。

二つの所有UI環境は終了APIから正常終了し、作成したタブを閉じた。Windowsアプリの再起動・スリープは追加していない。

## 実接続と画面の境界

#28で別に承認された二要求とGitHub作用を下記の範囲で実行した。#27の三要求枠は流用していない。既存の消費カウンタを保持し、未承認の追加要求・外部変更を行わない。専用キーや秘密鍵を文書・ログ・Gitへ記録しない。

共通UIではブラウザで一巡し、新しいOS境界を変更しない限りTauriで同じ操作を重ねない。ユーザーが提示した入口から参照される`DESIGN.md`と`docs/adr/0014-uiux-dev-flow.md`は、このcheckoutには存在しなかった。既存のフォーム・履歴・権限表示を使い、指示書やデザイン文書を推測して新設しない。

初回commit `ea4f2eb`のCI（run `39087728605`）はShared UI and server・Windows native client・GitGuardianがすべて成功した。独立検品の修正後のHEADについては、別途CIを確認する。`issue28-review-check.log`の静的検査もPASS。

本人はContext取得固定版の初期上限と二つの実試験を承認した。変更の実施と消費の範囲は以下へ記録する。

## Scope revision 2の確認

本人の承認後、入力組立てで選択・除外から読む固定版を初期10万件に制限した。同じ版は再利用し、次の取得前に検査する。`KOUJOU_CONTEXT_MAX_LOADED`で変更可能。API起動時の同じContext serviceをTriageへ接続し、配置数・byteとは別に適用する。Context保存の制限は追加していない。

`issue28-load-bound-before.log`は上限3の設定で6版を取得して成功してしまう変更前の失敗。`issue28-load-bound-after.log`では選択・除外とも3版までで停止し、同じ版の再利用は1取得となった。`issue28-load-bound-triage.log`では超過時のモデル要求ゼロ・入力待ち・採用済みP1の保持を含めPASS。`issue28-historical-continuation-owned.log`は旧書込み意図のない履歴の競合保護、明示解消、App反映、外部手動固定の順を含めPASS。

`issue28-approved-check.log`、`issue28-approved-test.log`で静的検査、Go全パッケージ、共通UI 18ファイル64テストがPASS。CI run `39088352381`は、前回の診断削除時に残したGoの空白で失敗した。対象をgofmtで修正した。ContextとJWTを共有する既存経路も対象を絞って再確認する。

## 実OpenAIの二要求

承認済みの専用APIキー・Bifrost・`gpt-6.1-sol`で通常の合成Issueと非信頼の優先度変更指示を各一回送信した。両方HTTP 200・P1採用、固定引用あり、工数・費用・影響人数は不明のまま。入力は2507/2630 byte、input tokenは475/505、output tokenは199/232。各入力4 KiB・出力2048 token・30秒、retry/fallbackなしの枠内である。

記録は`issue28-live.log`、`issue28-live.json`と二つの原応答JSON。reasoning tokenと単価は欠測で、費用を0としていない。消費の許可記録は二要求とも使用済みで、追加要求を行っていない。Gatewayは通常停止し、標準の接続一覧は空のまま。

## 実GitHubの観測と途中失敗

既存App・対象repo限定で合成Issue [#63](https://github.com/KingYoSun/koujou-san/issues/63)と二ラベルを作成した。最初はAppの書込みはできたが、イベントの`performed_via_github_app`がnullで由来判定に失敗した（`issue28-github-live.log`）。IssueのCloseと二ラベル削除を確認済み。App ID `5355530`、actorはbotの数値ID `343876039`だった。

App自身をJWTで照合し、そのslugから取得したbotの数値IDとイベントactorを照合するよう修正した。人・別App・App ID不一致の所有試験は`issue28-app-provenance-before.log`の失敗から`issue28-app-provenance-after.log`の成功へ変わった。`issue28-github-existing-event.log`で#63の既存イベント`33554004876`を読み取るだけでOwnと判定した。読み取りprobeのソースはGit管理外の`.cache/issue28-live/existing-event-proof.go.txt`へ保全した。

本人は同じClose済み#63の二ラベルを一度復元する追加枠を承認した。再開も収束せず失敗し、二ラベルを削除した（`issue28-github-resumed.log`）。書込み意図を持たない新しい検証DBと以前のイベントの組合せから、所有環境で`concurrent_manual_priority`を再現した。実試験時の最後の同期理由は未保存であり、コード経路と所有再現による原因判定である。競合保護を弱めず、通常の明示操作でこの競合を解消する手順をfixtureへ組み込み、最後の同期状態も記録するようにした。

現在の累積消費はIssue作成1・Close1、ラベル作成4・削除4、Appのラベル作用1、人の外部変更0、モデル0。承認枠の追加なしにラベルを再作成しない。既存のカウンタをリセットせず、継続する場合も同じIssue・同じ二ラベルだけを使う。実GitHubの外部手動変更・固定の最終判定は未確認。

`issue28-contexts-regression.log`と`issue28-publisher-regression.log`は既存PG試験がPASS。追加差分の独立検品は固定`37659e4`でPASS、blocker 0件。最新CIはrun `39095566623`で進行中。GitHub二ラベルの追加作成・削除を各一回だけ求める質問を提示済みで、回答前に三回目の実試験は実行しない。

## 実GitHubの最終結果と全条件の判定

本人が同じ#63の同じ二ラベルをもう一度だけ作成・削除する第二追加枠を承認した。再送された第一追加枠の回答は重複として扱い、カウンタをリセットせず追加回数だけを記録した。`issue28-github-final.log`はPASS、`issue28-github-live.json`はApp由来識別、履歴競合の明示解消、外部の人の変更からP2固定、最終同期synced、本文・状態不変更、モデル要求0を示す。

累積消費はIssue作成1・Close1、ラベル作成6・削除6、App作用2（総上限4以内）、外部手動変更1。#63を再Openせず、最後の読取りでもClose・ラベルなしを確認した。二つの一時ラベル自体もrepositoryから削除済み。OpenAIの消費は二要求のままである。

別コンテキストの検品者が最終のログ・結果JSON・累積カウンタと検品済みコードの一致を照合し、AC-1〜13・INVAR-1〜4すべてPASS、blocker 0件として検品を終了した。過去の失敗と原因未確定のHopper観測は消さない。#33へ移した本人の初見操作・S3スリープは未確認のまま維持する。

HEAD `82781f9d2d4dce9f65c512d3841613be23588b8d`のCI run `39095730596`はShared UI and server・Windows native client・GitGuardianすべてPASS。現在の残作業は、この最終文書を含むHEADのCI確認、マージ、tree照合と手動Closeである。
