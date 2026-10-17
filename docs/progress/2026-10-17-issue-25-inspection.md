# Issue #25: 独立検品

## 対象と入力

- 対象commit: `ab292f1a4e5eec36065ba77cbf3280b59c59ca65`。
- 基準main: `a7c8b56151f51e876844ae2810f59d561b81a631`。Scope revision 1、区分C。
- 検品者: 実装会話を継承しない独立担当。実施日: 2026-10-17。
- 固定条件: [計画](2026-10-17-issue-25-plan.md)のAC-1a〜10b・INVAR-1〜3。
- 入力: AGENTS.md、AGENTS.local.md、文書案内、開発手順、Issue手順、ADR 0025、Hopper runbook、[実装証跡](2026-10-17-issue-25-evidence.md)、[公開UI抜粋](assets/issue25-ui.json)、対象差分、`test-results/issue25-*`のログ・UI画像・評価JSON。
- 秘密fixture、`.cache`の秘密値・鍵・OAuth・端末ファイル、プロバイダ資格情報、他案件履歴は読んでいない。外部write、新しいApp・実Issue、実CLI・OS・ブラウザの再操作は行っていない。
- `.codegraph/`があるため、コードはCodeGraphのexplore/nodeから確認した。索引対象外のSQL・文書・保存済みログは直接読んだ。
- source・testは変更せず、この記録だけを作成した。新規試験・追加要件は持ち込んでいない。

## 条件ごとの結果

既存ログの成功は、そのfixtureが通った証拠として扱う。下記のFAIL経路は既存fixtureに含まれず、固定HEADの呼出しとSQLから確認した。試験を追加して実行した結果ではない。

| 条件 | 確認した経路・証拠 | 判定 |
| --- | --- | --- |
| AC-1a | `condition.go`はCI結論、終了コード、検品判定を別に取得する。`condition_test.go`とunits/postgresログはfailure・cancelled・timed_out・未知値・終了コード0の分離を確認している | PASS |
| AC-1b | allは偽、anyは真で順番に短絡し、それ以外で不明を伝播する。真・偽・不明の分岐を有限に照合した。条件はeq/in/all/any/notだけで、Gatewayやshell呼出しはない | PASS |
| AC-2 | owned_HTTP fixtureは観測・共有確定・Worker・二周期の発行まで成功し、Runner不在と復帰も同じRunを使う。一方、受理済み旧周期をWorker復帰後に処理する経路がB2で失われる | FAIL |
| AC-3 | proposal/autoとrequest-startを分離し、共通Run.Create/Action/Assignへ接続する。実行許可失効時のsavepoint rollbackと明示発行はpostgresログのexecution_permissionで成功している | PASS |
| AC-4 | 同一通知、並列Worker、発行後消失・lease回収、起票応答喪失は既存postgresログで成功している。確定前に停止したWorkerの旧周期仕事はB2で処理されない | FAIL |
| AC-5 | Workspace stream lock下で問題キー・枠・発行を同じtransactionで確定する。Issue発行の並行関連付け・再発提案は成功しているが、許可されたRun発行定義の同一問題照合はB1で失敗する | FAIL |
| AC-6a | events/publishの由来継承と、workerの深度・累積発行・評価数・期間の各境界を照合した。cycleとIssue_Workflowの有限fixtureは同じ起点・深度と累積2件、以後の停止を確認している | PASS |
| AC-6b | 固定した由来はPGからArcadeDBへ投影され、Issue Scope→Workflowの関係取得は成功している。通常Workflowが先に完了した場合の再試行重複はB3で残る | FAIL |
| AC-7 | CheckJobTxでlease・現在主体・管理設定・予算・有効版を再確認する。予約後の費用0・失効・実行拒否は未発行となる。確定済みIssue/Runを削除する処理はない | PASS |
| AC-8 | requiredのunfetched/missingは入力待ち。テンプレートの固定Sources・Selectionを共通Context処理で確定し、noneへのcontext mappingは拒否する。required_local_evidence、Issue_Workflow、MCPのnone/exclude境界と照合した | PASS |
| AC-9 | 保存・有効化は新しい受理だけを対象にする。過去評価はsource key索引のページで発行せず保存する。履歴はposition/idの組で継続する。saved_definition、cycle、二周期とUI試行の証拠が一致している | PASS |
| AC-10a | HTTP/MCPは同じServiceへ接続する。実Ory・公式SDKの固定版と再送が一致した既存ログを確認した。共通UIの保存・有効化・発行履歴・過去試行・停止再開はUIログと画像/JSONで照合した | PASS |
| AC-10b | Read/Queryは現在のWorkspace・資源・分類・操作範囲を検証する。二利用者・別Workspace・非公開根拠・read副作用なし・edit/manage分離はpostgres/MCPログと実装が一致している | PASS |
| INVAR-1 | StoreのWorkspace lock、CAS、receiptとPGの予約を共通利用する。graph.PutRelationはPGへ保存し、外部グラフ同期は発行判定へ使わない。001〜019は差分になく、追加migrationは020だけ | PASS |
| INVAR-2 | Hopperは元Runを読み、発行先を共通サービスで作る。元の結果・原出力・判定を更新しない。通常RunとWorkflowの既存回帰ログも成功している | PASS |
| INVAR-3 | Hopper/Workflowは同じusage.ObserveRunCostsを使う。欠測・未算定・遅着・通貨/評価種別と現在の根拠認可を確認し、保証はfurther_starts_onlyとする。費用欠測を0へ変えない | PASS |

## 指摘

### B1: Runの同一問題予約をIssueとして読み、次の結果を確定できない

- 分類: blocker。条件: AC-5。
- 根拠: `hopper/service.go:149-184`、`hopper/model.go:80-81`、`hopper/worker.go:170-186,247-248`。
- 到達経路: 有効な`action.kind=run`と`problem`を持つ定義を保存する。初期Templateもこの組合せを返す。識別情報が同じ最初の結果はRunを発行し、`hopper_problems.destination`にそのRunの固定参照を保存する。照合期間内の次結果では、その参照を`publication_records`から必ず読むため`pgx.ErrNoRows`になる。
- 実害: 二つ目の評価transactionがrollbackされ、link_open/hold/recurのどれでも定義した関連付け・保留・再発に到達しない。再送しても同じ参照で失敗する。既存並行fixtureはIssue発行だけなので、この経路を検出していない。
- 再検品範囲: 許可する定義と問題予約の型を一致させた差分、同一問題を持つ二結果の判定・発行数、既存Issueの関連付け・再発・保留。利用場面を制限する修正は、受入条件・利用者体験への影響として先に判断を確認する。

### B2: Worker停止中に次周期を受理すると、保存済み旧周期の仕事が失効する

- 分類: blocker。条件: AC-2・AC-4。
- 根拠: `hopper/events.go:187-209`、`automation/service.go:395-415`、`automation/worker.go:60-75,158-177`。
- 到達経路: Hopper Workerが処理しない間に、同じGitHub観測対象の完了周期AとBを順に受理する。両方のsourceとjobは保存されるが、`hopper-result:<target>`のheadはBになる。復帰後のClaimはAのjobにも現在headとの一致を要求し、`basis_changed`でblockedにする。claim後・Execute前にBを受理した場合もCheckJobTxの同じ判定で停止する。
- 実害: 現在の権限・定義・費用が有効でも、既に受理した固定結果Aが評価・発行されない。保存済み仕事の復旧が、停止中に次周期を受理したかどうかで変わる。未観測周期の取得を求める指摘ではない。
- 再検品範囲: Hopperの固定結果検証とTriageの現在版検証を扱う差分、同じ対象の二周期を受理してからWorkerを復帰する順序、現在権限・有効版・leaseの既存拒否。

### B3: 通常Workflow完了後に遅延評価すると、実施済みの再試行を重複発行する

- 分類: blocker。条件: AC-6b。
- 根拠: `hopper/worker.go:150-160`、`workflows/retry_owner.go:27-63`。
- 到達経路: 通常WorkflowのRun Rが失敗し、Hopper jobの処理だけが遅れる。その間に通常Workflowが同じRunDefinitionの再試行を作成し、成功して工程を完了する。Rの固定結果は変わらない。後からHopperがRを評価すると、OwnsRetryTxは現在のexecutionがrunning/waiting以外であることだけでfalseを返し、既に担当した再試行を認識しない。
- 実害: 同じ失敗結果と再試行先に対して、通常Workflowが実施済みの修正RunをHopperが追加発行する。既存fixtureは工程が未完了の時点で抑止を確認するため、完了後の順序を検出していない。
- 再検品範囲: 既存再試行の所有を判断する差分、通常再試行が先に完了した後の遅延評価、未完了工程での既存抑止と通常Workflow再試行。

## 証拠の照合と限界

- `issue25-postgres.log`は15ケースすべてPASS。`issue25-mcp.log`は実Ory・公式SDK suiteがPASS。UI suiteも`issue25-ui.log`で487.70秒のPASSを確認した。
- published画像と`issue25-ui-evaluation.json`は評価ID `76681a84…885bf`、版`2SVLY22I524OB37HPC2TLQQJTY`、position 30200、state published、深度0、発行先Runが一致する。dry-run画像は発行なし・未発行で採用/開始ボタンがない。resumed画像と再開後JSON・公開抜粋は版`QAC6Y7X2CH2ARX3XBO7BBIZ4HO`、position 30205、reason assignment_resumed、同じ発行先Runで一致する。
- `issue25-check.log`、`issue25-test.log`、database/runs/workflow/publication/automation/usageの各既存回帰ログは成功を確認した。条件未実装、由来の深度、試行履歴ページに関するbeforeログの失敗は、後続の成功ログと区別した。
- 独立検品では新しい試験や既存suiteの再実行は行っていない。上記3件はソース経路による指摘で、実行再現済みとは記載しない。
- 最終head CI、merge後照合、実CLI・Tauri実機・OSの再操作は未実施。新しいAppや実Issueの作成・外部writeはしていない。

## 判定

総合判定はFAIL、blockerはB1〜B3の3件。全末端を上表で判定し、指摘を先に実装担当へ通知した。修正後は各指摘の再検品範囲に限定して確認する。

日本語はyomiyasuの指針に従い、条件・根拠・未実施事項を変えずに推敲した。lintは95/100で、箇条書き比率の候補1件を確認した。対象・根拠・影響・再検品範囲を並べる記録のため、箇条書きを維持した。

## 修正差分の再検品（2026-10-18）

対象commitは`5ae982fcb9dd5475c662e2618e53138711f21169`。基準は原本の`ab292f1a4e5eec36065ba77cbf3280b59c59ca65`とし、B1〜B3の実装・試験差分、実装証跡の追記、指定された既存ログだけを確認した。原本のFAILは旧commitの判定として残し、現在の判定をこの追補へ集約する。

| 指摘・条件 | 差分と確認結果 | 判定 |
| --- | --- | --- |
| B1・AC-5 | `hopper/worker.go:181-190`はRun/Issueの現在の完了状態を照合し、固定参照と依存の認可には既存`Runs.SourcesTx`を使う。Run参照をpublication_recordsだけから読む経路を除去した。before-3ではlink_open/hold/recurすべてが二結果目でno rowsになり、afterではそれぞれlinked/held/publishedへ到達する。link_openは元の同じRunを参照し、そのRunの完了後は再発提案になる。既存Issueの並行関連付け・再発提案・起票応答喪失も同じsuiteで成功した。定義の利用範囲は制限していない | PASS・解消 |
| B2・AC-2/4 | `automation/worker.go:60-75`はHopperに限り、共有変更へ保存された固定結果版を許可する。管理主体・現在の参照権限・有効版・lease・予算の検査は維持し、Triageは現在版一致と現行方針の検査を維持する。before-3では最初の受理周期をClaimできず二周期目が返り、afterでは二周期を先に受理した後も最初のjobをClaimできた。さらにClaim後に三周期目を受理してからExecute/Finishし、三周期すべての発行を確認している。既存の失効・予算・旧lease拒否も成功した | PASS・解消 |
| B3・AC-6b | `workflows/retry_owner.go:29-63`は保存済みRetriedと同じnodeの後続attemptを確認し、工程完了後も元結果・再試行先の固定RunDefinitionを照合する。before-3では完了済みの再試行に対してpublishedとなり、afterではheld/workflow_owns_retryとなって発行先を作らない。fixtureは通常Workflow.Stepが再試行Runを作り、そのRunの合成完了結果を受理して工程をcompletedにしてからHopperを処理する。未完了工程での既存抑止・通常再試行も同じsuiteで成功した | PASS・解消 |

`issue25-inspection-before-3.log`は3件の指摘を再現した失敗記録として確認した。`issue25-inspection-before.log`には試験配置による後続ケースへの干渉、`issue25-inspection-before-2.log`にはfixtureの固定版準備の失敗があり、3件の確定した再現とは区別した。

`issue25-inspection-after.log`は18ケースとRun問題方針の3下位ケースがPASS、packageは11.536秒。共通処理の利用側は`issue25-inspection-automation.log`の5.021秒、`issue25-inspection-workflows.log`の8.781秒のPASSも確認した。固定差分と既存ログで判定できたため、独立担当による追加試験・実機操作・外部writeは行っていない。source/testは変更せず、この追補だけを保存した。

現在の総合判定はPASS、blockerは0件。AC-2・AC-4・AC-5・AC-6bをPASSへ更新し、原本のその他のPASSを維持する。必要な再検品範囲を満たしたため、ここで検品を終了する。最終head CIとmerge後照合は未確認であり、Issue全体の完了判定には含めていない。

追補の日本語もyomiyasuで推敲し、条件・根拠・未実施事項を保って確認した。
