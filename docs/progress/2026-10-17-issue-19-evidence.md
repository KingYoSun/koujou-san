# Issue #19の検証記録

対象と有限な判定条件は[計画](2026-10-17-issue-19-plan.md)、採用方式は[ADR](../adr/0019-execution-reconciliation.md)、現在の判定は[current-state](current-state.md)を参照する。実装開始点はmain `8e067b6552e3a7260dc959fe547c0422b8044ec3`。

## 実環境

Windows、Node 22、Go 1.27.2、Rust 1.98.1、Python 3.13、Ubuntu-22.04のWSLを使用した。試験専用PostgreSQL・Ory・APIと、所有する固定スクリプトだけを操作した。標準の開発API・Webや他のアプリを停止していない。

API試験はTLSのHTTPサーバーを実際に停止し、同じアドレスと証明書で再作成した。Run ServiceとPG接続poolも作り直し、同じ試験DBの確定記録へ再接続した。Go試験process全体やOSを再起動した試験とは区別する。受理後の応答消失は、報告のcommit後にHTTP接続を閉じて再現した。

## 条件との対応

| 条件 | 観測と結果 |
| --- | --- |
| AC-1・2a | Windows CLI PID 46040、WSL中継PID 51224 / ゲストPID 219774。両方でAPI停止中に原出力4件が未送信となり、同じPIDの連番が進んだ。復旧後は各10連番・stderr 1件・終了コード0が同じRunへ揃い、追加起動0。報告順序15まで受理。 |
| AC-2b・4a | Chromeの二利用者で実操作。Aliceの経路を切断して回答案を保存し、Bobの正式回答を受理・配送・再開した。古い画面からの送信を拒否。再読み込み後は古い案を送信不可で復元し、二つ目のPauseは別の空欄として表示。明示回答後は二Pauseがresolved、Run正常終了・報告17。旧案はCLIへ配送されていない。 |
| AC-2c・3a | テストbuildで実際のCLI spawn直後、対応metadataの保存前にRunner processを終了した。起動意図はstartingのまま、既存CLIと副作用ファイルは継続。製品Runnerの再起動はUnknownを記録し、追加CLIを作らなかった。障害注入はテストbuildだけに含む。 |
| AC-2c・3b | 稼働中のRunnerを失わせ、同じCLIのheartbeat継続を確認。新RunnerはPIDとOS作成時刻で直接processを観測したが、stdio再接続は未確認としてUnknownと最後のrunningを保持。未確認PIDの記録と期限切れ・二端末の再配布拒否も確認。 |
| AC-4b | 未開始の割当を保持した状態で取消、または依頼者の実行許可を取り消してRunnerを起動。実CLIと副作用ファイルの作成0。取消要求は実終了確認へ変換せず、未確認時は最後のqueuedを保持。 |
| AC-5 | 共通画面で正常終了したRunの出力1件を選び、固定参照と新しい指示を別Runへ保存。作成時は未依頼。割当時に終了確認の判断者・時刻・元Run版を記録。実CLI正常終了。実PGでは固定添付と別worktreeも照合。 |
| AC-6 | Unknown元Runの引継ぎを画面で準備し、重複了承なしの割当を拒否。明示了承後にduplicate_possibleを記録して別Runを実行。実PGでは元出力・完了の遅着後も、新Runの版・入力・状態が不変。 |
| AC-7a | API停止中も同じCLIのheartbeatが継続。復旧後の取消要求から実際のchild終了を確認してCancelledにした。既存の副作用は保持。画面終了後の独立継続は#18の実機証跡を維持。 |
| AC-7b | 未確認。S3対応は確認済み。本人の実施タイミングを得てから実OSのスリープと復帰を試す。 |
| INVAR-1〜3 | 上記の停止・古い要求・権限取消で、通信断をPauseや完了へ変換せず、未受理回答の配送・同一Run再発行を行わないことを確認。 |

ネイティブ画面の新しい「実行記録を照合」操作は未確認。実スリープと合わせて残しており、Issue #19の完了やmergeの条件を満たしたとは扱わない。

## 保存・順序・検証

`pnpm run test:runs`は専用PGで成功。報告の欠落順序・逆順・新IDでの同じ順序を拒否し、同じoperation IDの再送は同じreceiptを返した。引継ぎでは古い元Run版・権限不足・判断なしを拒否し、固定入力・添付・専用worktree・遅着の分離を照合した。

独立検品は初回head `e0dd23f286e2289c06fa7461ac43a174e60cc2ff`で、Runner消失後の保存済み出力が観測をknownへ戻す経路を指摘した。以前の観測更新へ戻した実PG試験は`stored report invented current session continuity output known`で失敗した。割当へセッション未確認を永続記録する修正後は、同じ出力・古いknown・receipt再送を受理してもUnknownを保ち、終端だけを確認済みの事実として扱う試験が成功した。

Web保存の試験は、既存Context下書き1万件を保持したまま対象Pauseだけを読み戻す。API/利用者/Workspace/Pauseの分離、質問版の保持、失効時の保存待ちと遅着応答を確認した。WindowsではDPAPIの暗号化と旧同期DBの読戻し、journalの旧列構成と50件ずつの102件復旧、再発行拒否を確認した。

root check/test/buildとdesktop check/test/buildを実行した。rootの共通UIは11ファイル37試験、desktopは11試験成功。実Oryへの認証・端末登録は明示したignored試験を実行して成功。最終差分の検証・独立検品・CIは追記する。

画面証跡は[オフライン保存](assets/issue19-offline-answer.png)、[古い案と次のPause](assets/issue19-stale-answer.png)、[固定出力の引継ぎ](assets/issue19-fixed-handoff.png)、[Unknownの重複了承](assets/issue19-unknown-handoff.png)。構造化した実測は[実行記録](assets/issue19-reconnect.json)にまとめる。資格情報と端末内pathは含めない。

AGENTS.mdから参照された`DESIGN.md`と`docs/adr/0014-uiux-dev-flow.md`は、このcheckoutには存在しなかった。既存のRun画面と操作構造を使い、別のビジュアル仕様を作って代用していない。
