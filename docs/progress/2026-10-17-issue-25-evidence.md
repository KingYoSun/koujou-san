# Issue #25: 決定論Hopperの実装・検証記録

- 状態: In progress。Scope revision 1、区分C。基準main: `a7c8b56151f51e876844ae2810f59d561b81a631`。
- 固定条件は[計画](2026-10-17-issue-25-plan.md)のAC-1〜10・INVAR-1〜3。条件の追加・緩和はしていない。
- 初期設定は、ユーザー回答に従い自動発行・通常の割当と開始、変更可能な起点別上限、未完了Issueへの関連付けと再発提案とした。
- 共通UIの実操作と独立検品を終了した。最終head CI、merge後照合は、この時点では未完了。

## 実装の境界

既存の受理transaction・永続Worker・CAS・receipt・現在認可・Run・共有起票を接続した。評価、起点の枠、同一問題、内部の作成を同じtransactionで確定し、外部作成は#22の作成確認・結果不明照合を使う。新しいCLI実行器や独自のGitHub書込み経路は追加していない。

Hopperは型付きの条件だけを評価し、LLMや任意shellを呼ばない。定義保存・有効化・過去の試行・発行・割当を分け、未取得のログは入力待ちにする。発行先へ実装履歴を暗黙に注入しない。

Runの自動開始は永続化した割当待ちから既存の割当へ進む。Runner不在では同じRunを保持し、復帰時も現在の主体・接続能力・受取権限を確認する。Repositoryとcommitを固定したGitのRunには、通常の専用worktree・branch情報を付ける。実プロセスとGitの準備は既存Runnerに任せる。

型付き結果の対象索引、過去結果のキー索引、起点の由来と発行予約を追加した。通常の処理でContext全件・全Run履歴を先に読む方式は使わない。既存migration 001〜019は変更せず、020だけを追加した。既存データの削除・変換は行っていない。

## 条件と主な証拠

ログは`test-results/`配下に保存する。PG/ArcadeDB/HTTPの合成fixtureと実サービスの境界を区別する。

| 条件 | 確認した結果 | 証拠 |
| --- | --- | --- |
| AC-1 | failure/cancelled/timed_out/未知値、AND/OR/NOT、観測終了コード0とCI結論の分離。Gatewayを用意せず判定 | `condition_test.go`、`issue25-units.log`、`issue25-postgres.log` |
| AC-2 | 所有HTTPでApp token交換と定期GitHub照合、共有結果確定、永続Worker、二周期のRun発行まで一巡。最初の連鎖停止は次周期へ持ち越さない。Runner復帰後も同じRunへ割当 | `observer_test.go`、`issue25-postgres.log` |
| AC-3 | 提案/自動発行、Run作成/開始を分離。実行許可を失うと部分Runを残さず評価を保留し、復旧後の明示操作だけで発行 | 同ログの`execution_permission_is_separate_and_failed_publication_is_atomic`、通常APIを使う`publish.go` |
| AC-4 | 同一通知で仕事が増えず、二Worker・lease回収・発行後の応答喪失でも同じ評価を利用。起票応答喪失後は共有の照合で確認し、作成は一回 | 同ログのreplay、parallel、committed_effect、Issue_response_loss |
| AC-5 | 別結果の同一問題を同時に扱い、一発行・一関連付けとなる。外部で完了後の再発は提案になる | 同ログの`parallel_problem_reservation_and_recurrence` |
| AC-6 | 起点と深度をIssue Scope・工程・別Hopperへ継承し、深度上限で停止。通常工程の同じ再試行先をHopperが重複発行せず、実ArcadeDBで固定した由来を探索 | 同ログのcycle、Issue_Workflow、`graphindex/lineage.go` |
| AC-7 | 確定前の費用枠0と管理者権限の失効で未発行のまま保留。既存結果・発行先を削除しない | 同ログのrevocation、execution_permission。既存の管理・予算処理を使用 |
| AC-8 | 未共有ログは入力待ち、元の実装履歴を新Runの入力へコピーしない。固定入力・noneを維持 | 同ログのrequired_local_evidence、Issue_Workflow、client_absent |
| AC-9 | 保存/有効化で過去を再発行せず、発行しない試行を別履歴へ保存。停止・上限と明示再開、次周期の起点を分離 | 同ログのsaved_definition、cycle、owned_HTTP |
| AC-10 | 実Oryと公式MCP SDKでHTTP/MCPの固定版と再送が一致。edit/manage、空の入力範囲、分類された根拠の拒否、参照で仕事が増えないことを確認。共通画面で保存・有効化・履歴・過去試行・停止再開を操作 | `issue25-mcp.log`、下記の画面証拠、`issue25-ui.log` |
| INVAR-1〜3 | PGで予約・予算・発行を確定。Runの結果をHopperが更新しない。欠測費用は未確定とし、追加開始だけを制限する | 通常サービスへの呼出し、各障害fixture、共通化した`usage.ObserveRunCosts`と既存Workflow試験 |

## 変更前の失敗と修正

`issue25-condition-before.log`は、条件処理が未実装でコンパイルできない状態を記録した。機能が動作していたとは扱っていない。実装後の同じ条件試験は成功した。

初期の統合では、ネストした共通保存の後に古いstream sequenceを使い、共有変更の主キーが競合した。保存後に同じロック下の最新sequenceを読むよう修正した。既存Run・起票・Worker・Workflowの専用PGも再確認した。

`issue25-lineage-before.log`は、子Runが通常の継承で先に得た深度を、新しいHopper発行が進められない失敗を記録した。通常継承は維持し、明示した新規発行だけ深度を一段進める修正後に、単一HopperとIssue/工程をまたぐ循環の停止が成功した。失敗と、その後のfixtureの後続失敗を成功へ読み替えていない。

HTTP一巡fixtureではtoken交換の合成応答を200にしていたため、実契約の201へ直した。MCPではtool catalogueの追加と、編集/管理目的のDB制約への追加漏れを修正した。これらの途中失敗を、最終の成功と区別する。

一括した過去評価の複数記録は同じ保存位置を持つため、位置だけの継続では履歴が欠けた。`issue25-trial-page-before.log`で1件ずつのページ取得による欠落を再現し、保存位置とIDの組で継続するよう修正した。変更後は同じfixtureで全試行記録を取得できた。既存の保存済み履歴は変更していない。

## 検証結果

- 専用PG・実ArcadeDB・所有HTTPの15ケースが成功。最新の`issue25-postgres.log`が最終結果で、保存位置が同じ試行のページ継続も含む。実GitHubや実CLIを使う試験ではない。
- `issue25-mcp.log`: 実Ory・公式SDKのHopper固定版・再送・目的分離・分類/none境界が成功。全suiteも成功した。
- `issue25-workflow-regression.log`: 通常の並列・再試行・判断・失効・外部条件・費用の専用PGが成功（package 12.283秒）。
- `issue25-publication-regression.log`: 共有起票・外部照合・現在認可の専用PGが成功（package 3.903秒）。
- `issue25-automation-regression.log`: 永続Worker・受理・枠・現在主体の専用PGが成功（package 7.179秒）。
- 静的検査・共通UI型検査・書式は`issue25-check.log`で成功した。以後の変更には最終検査を行う。
- `issue25-test.log`: Goと共通UIの16ファイル51試験が成功。固定参照へ補助項目を送らないUI回帰試験を含む。
- `issue25-database.log`: migration、共有保存、再送、並行更新、停止と復元が成功。`issue25-usage-regression.log`の既存利用量・料金・累積/遅着・1万件×3版も成功。

既存Appの実接続と、実Issue作成・再取得は#13・#22の証拠を再利用する。新しいApp・永続アクセス・実Issue操作は追加していない。Windowsアプリの再起動・認証・スリープ、実CLIの再実行は行っていない。Sourceの合成結果を実機結果へ格上げしていない。

## 共通画面の観測

Chrome、所有するOry/API/PGの共通Web画面で、一回の操作確認を行った。同じ環境には1万Context×3版があり、Hopperの対象選択や一覧でそれらの全本文を取得していない。定義の名称を変更して固定版を保存し、その版だけを有効化した。合成結果の受理後に発行履歴を取得し、キーボードのEnterで元結果・定義版・発行先・起点・深度へ進んだ。

過去の同じ結果を発行せずに評価すると、独立した試行記録が残り、発行先は未発行、採用・開始のボタンは表示されなかった。連鎖の停止・明示再開と、発行済みRunの割当待ちの再開も確認し、Run IDは変わらなかった。

最初の有効化は、UIが返却オブジェクトの補助項目まで固定参照へ含めたため拒否された。IDと版だけへ揃え、`hopperRef`の回帰試験と同じ画面の成功を確認した。JSON入力の読み上げ名も、本文ではなく短い固定ラベルにした。これらを含むビルドを再読込して確認しており、古いビルドの成功では代用していない。

画面は`test-results/issue25-hopper-published.png`、`issue25-hopper-dry-run.png`、`issue25-hopper-resumed.png`。元の固定版と再開後のJSONは`issue25-ui-evaluation.json`、`issue25-ui-evaluation-resumed.json`。[公開抜粋](assets/issue25-ui.json)には合成結果と参照だけを保存し、資格情報を含めていない。

最後に所有fixtureへfinishを送り、専用UI suiteは成功した（試験487.70秒、package 489.731秒、`issue25-ui.log`）。専用サービス・volumeは終了処理で削除され、秘密fixtureのJSONも削除した。共有CAと標準開発サービスは削除していない。共通画面とHTTPの確認をTauri実機で繰り返していない。

画面確認後の履歴継続の差分は、同じ位置の全試行記録を小さいページで取得する専用PGと、表示側の型・回帰試験で照合した。表示や操作の追加はなく、共通画面を再起動して繰り返していない。

## 独立検品後の修正

固定commit `ab292f1`の検品で、Run発行の問題照合、Worker停止中に受理した旧周期、通常工程が完了した後の再試行所有判定に3件の未達があった。`issue25-inspection-before-3.log`で、旧周期の欠落、Run照合時の保存先不一致、実施済み再試行の重複発行を再現した。それ以前の試験調整中の失敗は`issue25-inspection-before.log`と`issue25-inspection-before-2.log`に分けて残した。

同一問題の参照と認可は既存の`Runs.SourcesTx`へ統合し、Runの完了状態も照合する。Hopperは受理済みの固定版を評価し、Triageは現在版との一致を維持する。通常工程の再試行は、工程完了後も保存済みの再試行・後続attemptから所有を判定する。初期方針や利用範囲は変更していない。

修正後の`issue25-inspection-after.log`は成功（package 11.536秒）。同じ対象の二周期を先に受理し、Claim後にも次周期を受理してから三つとも一度ずつ発行できた。Runのlink_open・hold・recurと完了後の再発提案、通常Workerが再試行を完了してから遅延Hopperを処理する順序も成功した。既存のIssue重複照合、失効・予算・lease・循環・所有HTTPのケースも同じsuiteで成功した。

修正commit `5ae982f`の限定再検品はPASS、blocker 0件。[検品記録](2026-10-17-issue-25-inspection.md)に原本のFAILと解消根拠を残した。共通Workerと通常工程の専用PGも再実行して成功（`issue25-inspection-automation.log`、`issue25-inspection-workflows.log`）。`issue25-final-check.log`の静的検査・型・書式と、`issue25-final-test.log`のGo・共通UI 51試験も成功した。最終CIは別途確認する。

PR #59の初回GitGuardianは、テスト用DB名`koujou_hopper_test`をGeneric Passwordとして検出した。該当行の実際の資格情報は環境変数から取得しており、秘密値の埋込みはなかった。既存のGraph試験と同じく環境変数の取得を別行へ分け、PR内の古い同一行も再走査対象に残らないよう、この専用ブランチのcommitを整理した。検品済みの製品コード・条件・観測結果は変更していない。元の検品commitはローカルのbackup refに保持する。
