# ADR 0002: 共通API契約とクライアントの境界

- 状態: Accepted。
- 対象: [Issue #3](https://github.com/KingYoSun/koujou-san/issues/3)
- 根拠: 設計書§1.1、4.1、4.2、6.1、6.3、6.7、11.1、12.1、18

## 契約

Workspace内の業務APIとMCPは同じ業務serviceを使い、認可・版管理・更新記録を入口ごとに再実装しない。共通の型はGoの `internal/apicontract` とTypeScriptの `src/contracts.ts` に置く。既存healthzは業務APIの契約へ変更しない。

| 項目     | Go / TypeScript                    | 意味と所有者                                                                                                                           |
| -------- | ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| 対象     | `Reference`                        | Workspace ID・安定ID・版。文字列をそのまま保持し、クライアントで整数へ変換・並べ替え・最新版推測をしない                               |
| 更新     | `Mutation`                         | `operation_id` と `expected_version`。Workspace・認証済み主体・操作IDの組で再送を識別する                                              |
| 要求内容 | `RequestDigest` / wireには含めない | 正規化した対象・操作・前提版・payloadからサーバーが計算する。クライアントのdigestを信頼しない                                          |
| 確定状態 | `Meta` / `ResponseMeta`            | Workspace・更新位置・ページ継続・完全性。更新位置とcursorはopaqueな文字列であり、アクセス権ではない                                    |
| 完全性   | `Completeness`                     | `complete`、`partial`、`pending`。打切り・未反映・未取得を別の理由で保持する                                                           |
| 結果     | `Response[T]` / `ApiResponse<T>`   | dataまたはerrorのどちらか一つ。失敗を空の正常結果へ変換しない                                                                          |
| 失敗     | `Problem`                          | invalid_request、unauthenticated、forbidden、version_conflict、operation_conflict、unavailable。版競合では読取が許可される現在版を示す |
| 認可     | `Grant` / wireには含めない         | 認証済み主体と現在の対象権限からサーバーが作る。要求のroleやWorkspace所属を採用しない                                                  |

版は等価性の判定に使う。更新位置は取得した状態の基準であり、対象の版とは別に扱う。未取得の位置はnullとし、存在しない位置を作らない。`complete`には確認済みの位置が必要になる。next_cursorは同じ検索範囲・Workspace・基準状態に束縛し、続きの取得でも認可する。

完全性は要求した範囲に対する情報である。ページの取得完了だけで、全グラフの影響や全Contextの解決完了を保証しない。必要な参照・版・所属を揃えたManifestの確定は #7、投影の反映位置と部分結果は #9・#10 が扱う。

変更の処理順は、現在の認可、操作IDの保存結果と要求内容の照合、未処理要求の前提版照合、更新と結果の原子的保存とする。同じ要求の再送は前回の確定結果へ収束する。操作IDを異なる内容へ再使用すればoperation_conflict、別操作による古い版への更新はversion_conflictとなる。権限取消後に保存結果を再取得する場合も現在の権限を確認する。

`Grant`は対象リソースへの許可を業務serviceが導出する内部値であり、認証や永続化の代用ではない。実際のJWT/Ory検証は #5、Workspace・リソース・端末認可は #6、同一transactionでの更新と操作結果保存は #4 が実装する。

## 共有と端末固有情報

| 境界                   | 担う情報・処理                                                                     |
| ---------------------- | ---------------------------------------------------------------------------------- |
| Go API / PostgreSQL    | 共有する業務状態、認可、採用版、更新位置、操作結果、共有履歴                       |
| Ory                    | 認証情報・セッション・OAuth発行状態。業務上のWorkspace権限とは分ける               |
| Bleve / ArcadeDB       | 正本から再構築する検索・探索。権限と取得の完全性はAPIでも照合する                  |
| 共通UI / Web           | 許可された表示・編集・操作要求。OSプロセスや端末の秘密値へ直接依存しない           |
| Tauri / Runner         | ローカルCLI・パス・未送信記録・端末資格情報。共有APIには必要な記録と論理参照を送る |
| クライアントLLM操作    | 選択・生成・変更案と操作履歴。RunやサーバーWorkerへ置き換えない                    |
| Hopper / Triage Worker | サーバー側の評価・発行・優先度。共通業務serviceの認可と版管理を使う                |

Userのissuer/sub、Workspaceの所属、端末資格情報、CLI自身の認証、Gatewayキーを一つの資格情報へ統合しない。トークン・実パス・CLI履歴は共通の応答DTOやUIの共有状態へ含めない。

`clientCapabilities`は現在のsurfaceを示す。nativeという表示だけでは実行権限を与えず、未実装Runnerを利用可能と返さない。現時点でWebと共通UIはCLI起動や資格情報取得のAPIを持たない。

## fixtureと検証

Goの `cmd/contract-fixtures` が読取、更新、再送、版競合、操作ID衝突、別Workspace/更新権限の拒否、部分取得、投影待ちをJSONで返す。Goの出力と `contracts/fixtures-v1.json` の一致をテストし、rootのtestコマンドは最新のGo stdoutをTypeScript decoderへ渡す。frontend単体テストは同じ保存fixtureを読む。

fixtureはPostgreSQLや本番認証の実装ではない。JSONで合意する境界と、クライアントが版・失敗・不完全性を保つことを検証する。ID/版の数値精度が失われる変換、dataとerrorの混在、異なるWorkspaceのdata、理由のないpartialは拒否する。

## 各Issueで決めること

2026-10-15のユーザー判断で、初期デスクトップはWindowsから開始し、WSL内CLIも接続対象とした。Linux/macOSのデスクトップ対応は初期対象に含めない。WindowsのCodex・Claude Code、WindowsからWSLへ渡す標準入力と出力、Tauri callback・PKCE・DPAPI保存の先行結果は[検証記録](../progress/2026-10-15-issue-3-evidence.md)を参照する。これは本番RunnerやOry認証の完成を意味せず、具体的な能力・保存先・復旧は担当Issueで検証する。

| 担当                         | 残る判断と終了条件                                                                   |
| ---------------------------- | ------------------------------------------------------------------------------------ |
| #3                           | 初期OS・実行場所。最小試作と制約を示して採用判断を得る                               |
| #4                           | DB接続、migration、版/更新位置、保存結果の原子性。実DBの競合・停止・再送・復旧を照合 |
| #5・#6                       | 認証・失効、権限、端末保存。採用OSの正常/拒否/取消を実機で確認                       |
| #7                           | Context/Grouping/選択/Manifestの詳細schemaと原内容の再構成                           |
| #8                           | snapshot/差分の通信方式、履歴不足・下書き・再接続の照合                              |
| #9・#10・#11                 | エンジン接続・規模・完全性、MCPの採用版と認可接続                                    |
| #12・#13                     | Context操作の共通GUI、GitHubの観測と対象版への対応付け                               |
| #16〜#25                     | Command/Pause/派生/割当/実行復旧・工程・検品・外部操作・利用量                       |
| #14・#15・#26〜#28・#30〜#32 | 利用量/料金・Worker・LLM操作・Gateway・Hopper/Triageの詳細契約                       |
| #29・#33・#34                | 配布・運用・ライセンス、初期一巡と別プロジェクトの受入                               |

ここではドメインの詳細schemaや状態名、数値上限、同期transport、Gateway採否を確定しない。実装したwire契約を変更する場合は、適用範囲と互換性を確認し、必要な版・移行条件を記録する。
