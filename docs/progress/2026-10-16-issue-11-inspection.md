# Issue #11の独立検品

[実装手順](../runbooks/issue-lifecycle.md)と[雛形](../templates/inspection-record.md)に従い、固定した成果物を確認した。

## 対象と入力

- 対象commit: `941c611969a7c5a56575c181ed9d75984eebe88a`。基準main: `1f9acedb7cd091ef9da10c5bb8c4fd665b991305`。
- 関連Issue・条件: [Issue #11](https://github.com/KingYoSun/koujou-san/issues/11)、Scope revision 1、区分C。AC-1〜6とINVAR-1〜5を全て判定した。
- 検品者・実施日: 実装担当と分けたCodexの独立検品コンテキスト、2026-10-16。
- 目的・対象: 認証済みMCPから既存Contextの検索・探索・固定版取得・編集へ至る経路、送信禁止分類、操作範囲、記録、および変更した共通認可・検索・入力確定の回帰。
- 対象外: 外部アプリのDCR、MCP Host、モデル推論、未実装Domain、任意SQL・graph更新・shell・merge。通常の送信は逐一許可を求めず、禁止分類を自動適用する。同梱Web/Tauriの事前登録を前提とした。
- 入力: `AGENTS.md`、`AGENTS.local.md`、文書案内、開発手順、Issue実装手順、Issue本文、[計画](2026-10-15-issue-11-plan.md)、[検証記録](2026-10-16-issue-11-evidence.md)、[ADR 0010](../adr/0010-mcp-boundaries.md)、[MCP手順](../runbooks/mcp.md)、設計書§6.10・8.7・9.5・12.1・12.4、固定commitの差分とコード、Computer Useの画像証跡。
- 引き継がなかった情報: 実装セッションの会話履歴と判断過程。検証記録の自己評価だけで合否を決めず、固定commitの経路確認と試験の再実行で照合した。GUIは依頼で認められた既存の実操作証跡を使用した。
- 実行環境: Windows、PowerShell、Node.js v22.14.0、pnpm 10.16.1、リポジトリ管理のGo 1.27.2、Docker Engine 29.5.2。実PostgreSQL・Ory Kratos/Hydra・Bleve・ArcadeDB、公式Go SDK v1.8.0、交渉したMCPプロトコル`2026-07-28`。

作業開始時と試験終了時のHEADが対象commitと一致し、検品記録の作成前に追跡対象の変更がないことを確認した。コード探索は既存のCodeGraphから開始した。通常環境のデータへ接続せず、各コマンドが作る専用Compose projectで試験した。秘密値は出力していない。変更はこの検品記録だけである。

## 条件ごとの結果

以下のコード参照は、全て対象commit上のものを示す。試験内の行番号はログと照合できる。

| 条件 | 確認した経路・操作 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | `/mcp`の401 challengeからprotected resource metadata、Ory metadata、同梱client事前登録、ブラウザ認証・PKCE・broker、resource付き交換、Streamable HTTPへ接続。通常API token、ID Token、期限切れを拒否 | `mcpapi/service.go:80–110`、`auth/service.go:195–269`、`auth/http.go:316–345`。再実行した`test:mcp`の`integration_test.go:249,354,652`がPASS。初期接続時の交渉版もassertされる。`test:auth`で通常認証・復旧・TOTPの回帰なし | PASS | 外部DCRは対象外 |
| AC-2 | GUI→通常HTTP→`Contexts.Put`と、MCP→同じ`Put`→StoreのCAS・receiptへ接続。通常HTTPの成功要求を同じ操作IDでMCPから再送し、同じ保存版になる。別操作IDで古い元版を送ると競合し、履歴は3版のまま | `contexts/http.go:73–104`、`contexts/service.go:180–214`、`mcpapi/service.go:245–252`。`test:mcp`の`integration_test.go:394–502`、`test:contexts`がPASS。GUIは[実操作記録](2026-10-16-issue-11-evidence.md#共通uiの実操作)と[画像](assets/issue11-ui-policy-and-context.png)の本文・保存版を照合し、`sync-engine.ts`の競合・明示的再適用経路とも一致 | PASS | GUIの実操作は今回再実行せず、提供されたComputer Use証跡を使用 |
| AC-3 | access tokenと別にUser・Workspaceへ結び付く操作handleを要求。read/edit、固定版、別Workspace・別User、none/exclude、失効を現在認可で確認。toolとresourceの双方へ適用し、別操作のcursor・resource URIの再利用を拒否。分類変更でcursorの主体も変化 | `access/operations.go:65–196`、`access/service.go:165–280`、`mcpapi/control.go:49–89`、`mcpapi/service.go:51–78,143–174`。`test:mcp`の`integration_test.go:354,502,564`と`test:access`の`operation-boundaries`がPASS。resourceはprivate・TTL 0、HTTPはno-storeを照合 | PASS | Host内の対話・cache制御そのものは対象外 |
| AC-4 | Bleve候補を順序付きページで走査し、現在認可を通った1ページと継続判定分だけを収集。MCP候補には本文を含めず、件数・固定版・位置・完全性を返す。ArcadeDBの上限・部分結果、未投影固定版、エンジン間の位置不一致、実停止では完全な入力へ確定しない | `searchindex/query.go:89–257`、`graphindex/query.go:334–495`、`mcpapi/service.go:192–228,263–324`、`contexts/candidates.go`。`test:mcp`の`integration_test.go:502,564,601,632`、`test:search`、`test:graph`がPASS。1万Context×3版・半数禁止で許可候補100件を2.0346638秒、継続ありで返した | PASS | この2.035秒は1要求の観測値で、p95ではない。既存の負荷JSONは照合のみ |
| AC-5 | 分類変更・他Workspace取得・shell・merge要求を含む本文を通常データとして取得。追加のshell要求、read操作からの編集、分類タグを変える編集を拒否。toolの登録集合と操作権限を本文から生成しない | `mcpapi/service.go:44–50,230–252`、`access/operations.go:198–200`、`contexts/service.go:166–178,231–236`。`test:mcp`の`integration_test.go:282–354,414–420`がPASS。tool公開は5個に固定され、control APIは通常認証からのみ利用 | PASS | モデル推論・原ログのDomainは対象外。本文の注入指示と追加API要求のfixtureでサーバーの拒否を確認 |
| AC-6 | 成功したtool/resourceの返却前に操作境界と参照版を再確認し、操作・User・method・ID/版・返却範囲・反映位置・時点・編集操作IDを記録。固定版から添付・外部参照を追跡できる。resolveはPreviewを呼び、Manifest保存やRun開始を行わない | `mcpapi/service.go:143–174,263–324`、`access/operations.go:208–234`、`008_operations.sql`、`mcpapi/control.go:104–151`。`test:mcp`で履歴を読み、token・handle・fixture本文を含まないこと、プレビュー後のManifest件数0、再送後の保存版数をassertしてPASS | PASS | Hostの探索会話保存は対象外。共有状態への自動混入がない経路を確認 |
| INVAR-1 | GUI/HTTP/MCPが`Contexts.Read/Put/Preview`、`Search.Query`、`Graph.Query`と共通Accessを使う。検索・探索の候補選択照合は`CandidateInput`を共用。通常Principalでは操作分類を追加しない | 上記の呼出経路と基準mainとの差分。`test:access`、`test:contexts`、`test:search`、`test:graph`、通常UI24件、`test:mcp`が全てPASS | PASS | なし |
| INVAR-2 | MCP adapterは業務サービスだけを保持し、モデル通信・Host・Samplingの実装を含めない | `mcpapi.Service`の依存・登録tool・各処理と`cmd/api/main.go`の登録経路を確認 | PASS | モデル接続は対象外 |
| INVAR-3 | 操作Principalが許すactionはread/get/searchとedit目的のupdateだけ。execute・register・manageを拒否し、Context編集から実行経路へ接続しない | `access/service.go:165–183`、`access/operations.go:198–200`。`test:access`で独立したexecute権限と操作Principalのexecute拒否、`test:mcp`で未知tool拒否がPASS | PASS | なし |
| INVAR-4 | 通常HTTPで読めるContextも、操作Principalでは現在タグ・取得する歴史版タグ・直接所属Scopeの禁止を別に適用。管理者が規則を更新し、MCPからは変更不可。禁止の子を含む構成は取得拒否 | `access/operations.go:144–195`、`contexts/service.go:129–133`。`test:access`で通常閲覧継続・禁止Scope・歴史版・1万件分類、`test:mcp`で禁止Context/resource/構成の拒否がPASS。管理UIの証跡も照合 | PASS | 外部モデルへの実送信は対象外 |
| INVAR-5 | 本文を命令として処理する分岐がなく、tool公開・除外・編集目的・管理方針は認証済みcontrolと固定データから決まる | AC-3/5の拒否fixtureとサーバー経路、Manifest placementsからの固定範囲導出を確認 | PASS | なし |

## 指摘

| 指摘 | 関係する条件 | 到達経路と具体的な影響 | 根拠 | 分類 |
| --- | --- | --- | --- | --- |
| なし | AC-1〜6、INVAR-1〜5 | 固定条件の未達、または今回変更した共通経路による合意済み挙動の回帰は確認しなかった | 固定commitの経路確認と下記の再実行結果 | blockerなし |

## 判定

- 総合判定: **PASS**。AC-1〜6とINVAR-1〜5は全てPASS、blockerは0件。
- 再実行: `pnpm run check`、`pnpm run test`、`pnpm run test:auth`、`pnpm run test:access`、`pnpm run test:contexts`、`pnpm run test:search`、`pnpm run test:graph`、`pnpm run test:mcp`は全てexit 0。
- 主な実行時間: `TestOryIntegration` 11.48秒、`TestAccessPostgres` 1.29秒、`TestContextsPostgres` 1.50秒、`TestSearchPostgres` 13.41秒、`TestGraphPostgres` 20.20秒、`TestMCPIntegration` 60.15秒。起動・片付け時間は含まない。通常UIテストは5ファイル・24件PASS。
- 共通処理の回帰確認: 認証のaudience/scope・復旧・TOTP、Contextの四粒度・履歴・none/exclude・固定入力、現在認可・実行権限・端末失効、検索の40問・継続・歴史版・再構築、関係探索の両端・上限・固定入力・停止復旧を再実行した。
- 専用環境: 試験ごとのCompose projectは`0cb8e42d7438`、`879a7bd820e6`、`e5d912cb4ba5`、`4a806ff5c107`、`463b2a55a8d8`、`17fced76da04`（共通接頭辞`koujou-san-db-test-`）。全コマンドの片付けログと、各projectのコンテナが残っていないことを確認した。
- 未実行: GUIの再操作、`test:search-load`、desktop専用検証、build、実PR CIとマージ後照合。GUIと負荷は固定成果物に含まれる証跡を照合した。desktopのネイティブ実装は変更されていない。PR CIとマージ後照合は、この検品の対象commitを越える後続工程である。
- 修正後に再確認する差分・条件: 現時点で修正要求なし。固定条件を全て判定し、対象範囲のblockerがないため検品を終了する。

記録の日本語はyomiyasuの意味保持・情報の不増補・主述対応の観点で推敲した。
