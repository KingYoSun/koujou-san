# Hopperの定義・発行・連鎖

Workspaceの「Hopper: 結果から次の仕事を発行」で、固定した定義、評価、提案、発行先、連鎖の履歴を確認する。サーバーが処理し、画面を閉じても継続する。MCPからも同じ業務処理を使う。

## 設定する順番

1. 「工程の定義」でRunDefinition、または「外部起票」でIssueの下書きを保存し、その固定版を用意する。自由生成したcommandを、登録済みテンプレートの許可として扱わない。
2. 「Triage方針・優先度・処理状態」のHopper管理設定で、管理主体・有効状態・取得/確定・処理枠・試行と費用上限を設定する。管理主体には、対象の閲覧と、発行先の作成・実行に必要な現在の許可が必要。
3. Hopperの初期設定を読み込み、名称、対象、条件、発行先の固定版、入力対応、重複と起点上限を指定する。定義の保存だけでは有効にならない。
4. 管理者が保存した版を有効化する。対象となる新しい受理結果から評価する。過去は別の「発行せずに評価」で、一ページずつ確認する。

一覧は50件ずつ取得する。定義や評価の種類、起点で絞り込む。Workspace全体のContextを読み込んだり、Contextごとに許可対象を列挙したりしない。

## 定義の内容

| 項目 | 指定すること |
| --- | --- |
| `targets` | `kind=github`なら登録済み観測ID、`kind=run`ならRun IDまたは由来に含めたScope ID。`*`はその種類の全結果 |
| `condition` | `ci.conclusion / run.state / run.exit_code / inspection.verdict / result.<path>`の`eq / in`。`all / any / not`で組み合わせる |
| `action.kind` | `run`または`issue` |
| `action.template` | 登録したRunDefinitionまたは起票下書きのIDと固定版 |
| `action.start` | Runを通常の割当へ渡すか。初期値はtrue。falseなら作成後に待機する |
| `action.mappings` | 元の構造化結果の`path`から、Runの`prompt / context / command`、Issueの`title / body`へ明示した値だけを渡す |
| `action.repository` | Gitを使う自動割当のRepository IDと固定commit。`repository.commit`への明示mappingでもCI結果のcommitを固定できる |
| `action.parent` | 新しいIssue Scopeの親。指定しても権限は増えない |
| `mode` | `auto`または`proposal`。初期値はauto |
| `include_derived` | Hopper由来の結果も評価する場合にtrueを指定する |
| `required` | 評価に必要な固定証拠。未共有・未取得は入力待ちになる |
| `bounds` | 新しい起点の深度・累積発行・評価回数・期間・評価ごとの発行上限。初期値は3・10・100・86400秒・1 |
| `bounds.cost` | 任意の記録済み費用上限。`amount / currency / valuation`を指定し、欠測・遅着は保留する |
| `problem` | repository、target、rule、結果内のidentity_path、window_seconds、modeを指定。同一問題の扱いが不要ならnull |

`problem.mode=link_open`は未完了Issueへの関連付け、完了済みの再発は提案になる。`hold`は重複候補を保留し、`recur`は新規発行を許可する。比較するのは識別情報であり、文章の似た別の不具合を統合しない。

CIのfailure・cancelled・timed_outは別の値である。条件に含めたい値を指定する。未取得・未知値と判断不能を、一律に条件不成立へ置き換えない。観測commandの終了コード0だけでCI成功にしない。

## 待機・停止・再開

評価では成立・不成立・判断不能・入力待ち・提案・関連付け・発行・保留理由を区別する。過去の試行は発行しない記録として残り、その記録を採用して開始することはできない。

無効化は新しい評価と未確定の発行を止める。連鎖停止は対象の起点へ適用し、発行済みIssueの削除や開始済みCLIの取消しにはしない。起点の上限と累積値は工程や別Hopperでも維持する。既存の起点へ上限変更を適用する場合は、連鎖の明示再開で新しい上限を指定する。

保留した提案は、現在の許可と入力を確認して「この提案・保留を発行する」から再開する。自動割当が保留された発行済みRunは、管理設定を復旧後に「割当の保留を再開」を使う。既に割当・開始・終了したRunを再作成しない。Runner不在では同じRunが待機し、許可されたRunnerが戻った時に通常の割当を使う。

Runの結果・原出力・検品判定はHopperの判定で更新しない。検品の入力にも実装履歴を暗黙に渡さず、登録した入力・選択・除外・無注入を使う。

## GitHub Actionsの周期

既存の固定Runは`kind=actions, external_id=<Run ID>`で観測する。継続して新しい周期を見る場合は`kind=actions, workflow_id=<workflow ID>, event=schedule`を指定し、external_idとattemptは指定しない。既存の読取り専用AppとWorkspace bindingを使い、CIを起動する権限は要求しない。

workflowでは最新の対象Runを観測する。サーバー停止中などに観測しなかった全周期を保存済みとは扱わない。受理済み結果の履歴は無期限に保持し、過去の試行はその保存済み範囲をページ取得する。周期ごとにRun ID・実行回から起点を分けるため、一つの連鎖の停止で将来の周期を止めない。

## APIとMCP

- `GET /hopper/workspaces/{workspace}/template`: 初期設定。
- `GET/POST /hopper/workspaces/{workspace}/records/{id}`: 固定版の参照、CAS・操作ID付きの定義/有効化保存。
- `POST /hopper/workspaces/{workspace}/query`: `heads / history`、kind・ID・root・最大100件。続きは返された`after`と`after_id`を両方指定する。
- `POST /hopper/workspaces/{workspace}/evaluate-past`: 固定定義、種類、対象キー、継続位置を指定した発行しない評価。
- `POST /hopper/workspaces/{workspace}/decisions/{id}`: 提案のpublish、連鎖のstop/resume、発行済みRunの割当再開。

MCPは`hopper_get / hopper_query / hopper_set / hopper_evaluate_past / hopper_decide`を提供する。固定参照を使い、編集は`hopper-edit`、管理は`hopper-manage`の目的を通常認証側で用意する。MCP自身から目的や権限を作れない。read/Context editは発行権限ではない。

`pnpm run test:hopper`は所有するPG・ArcadeDB・HTTP fixtureで一巡と障害を確認する。新しいGitHub Appや実Issueの作成、Windowsアプリの再起動・スリープは必要ない。
