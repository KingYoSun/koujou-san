# Issue #14 独立検品

現在の判定は修正commit `38f903f46b3e873de2932ca45f04653f3979dfb6`に対して**PASS**、blocker 0件。下記の初回記録を保持し、末尾の差分再検品でB-1の解消と全条件の判定維持を確認した。

## 対象と入力

- 対象commit: `e0cfddbb8708a7778b4fc38129ffce4c37fcf762`。基準main: `1b46ec0cef76f50f7b92faa0a43d6c6d04a0b3e2`。
- 関連Issue: [#14](https://github.com/KingYoSun/koujou-san/issues/14)、Scope revision 1、区分C。
- 検品者・実施日: 実装会話を継承しない独立検品エージェント、2026-10-16。
- 目的: 利用量・料金版・費用評価・集計の全AC-1〜9、INVAR-1〜3を、[実装計画](2026-10-16-issue-14-plan.md)の末端fixtureまで判定する。
- 対象外: 実CLI収集#24、本格的な比較UI#32、単価・為替の自動取得、請求書照合、自動配賦、全CLIの内部消費の完全取得。
- 入力: `AGENTS.md`、`AGENTS.local.md`、文書案内、開発・Issue手順、検品雛形、Issue本文、設計書§7.10・9.8・12.5、[ADR 0013](../adr/0013-usage-pricing.md)、計画、[観測資料](2026-10-16-issue-14-evidence.md)、`assets/issue14-*`、対象commitの差分・実装・テスト。
- 適用したユーザー判断: 個別契約単価とその計算額もWorkspace共有を初期値とする。履歴は無期限に保持し、接続先LLMの中央完全管理を前提にしない。
- 渡さなかった情報: 実装者の会話履歴・自己評価。観測資料の結論だけで合否を決めず、コード、テストの判定内容、保存済みの画像・応答を照合した。指摘通知後に受け取った修正方針は、この固定commitの判定根拠に含めない。
- 実行環境: Windows、PowerShell、リポジトリのGo 1.27.2・pnpm、専用ComposeのPostgreSQL・Ory・ArcadeDB、公式MCP SDK。開始時と試験終了時のHEADは上記対象commit、追跡対象の変更は無し。
- 探索: `.codegraph/`が存在したため、コードはCodeGraphのexplore/nodeを先に使用した。今回参照した新規ソースは索引から取得でき、未索引を理由とする通常読みへの切替は無かった。文書、SQL、固定commit差分は直接確認した。`DESIGN.md`は存在しなかったため、未提供のビジュアル仕様を適合判定へ追加していない。

## 条件ごとの結果

以下の`PG`は、検品者が独立実行した`pnpm run test:usage`の`TestUsagePostgres`を指す。`MCP`は、独立実行した`pnpm run test:mcp`の`TestMCPIntegration`と`checkUsageMCP`を指す。表のPASSは記載した経路と有限fixtureに対する判定であり、対象外の利用場面へ一般化しない。

| 条件 | 確認した経路・操作 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1a 四帰属・未帰属 | HTTP保存・取得とPG payloadを五帰属で比較。要求モデルと実モデルは別欄、実モデル不明はnullのまま保持し、autoは実モデルとして受け付けない。取得元・変換版・計測範囲も原記録へ保持 | `usage/model.go:22-47,134-165`、PG `attributions_and_missing` | PASS | 実CLI収集は対象外 |
| AC-1b 未取得・0・非該当 | 同じ数量schemaでmissing/null、reported/文字列0、not_applicable/nullを保存・取得。集計SQLも数量、欠測数、非該当数を別に出力 | PG `attributions_and_missing`、`usage/query.go:164-168` | PASS | なし |
| AC-1b 欠測理由 | 計画の「nullと理由」を確認。QuantityにもObservationにも理由を保持する欄が無く、理由を付けた入力はstrict decodeで拒否される | `usage/model.go:16-47`、`usage/service.go:39-45,93-98`、指摘B-1 | FAIL | 実行による拒否応答の再現は未実施。型定義とHTTPまでの経路から判定 |
| AC-2a 内訳の重複除去 | 入力100がcache20を含む形式と通常入力80＋cache20の形式でともに0.182000。出力50に含まれるreasoning10も再加算しない | 独立通常試験の`TestDecimalConsumption`、PGの0.182000、`model.go:189-285` | PASS | なし |
| AC-2b 別単位 | 画像2×0.03をtokenとは別単位で計算し、合計0.242000。内訳・単価・数量の単位を保持 | `TestDecimalConsumption`、PG訂正・画像評価・FXの36.300000、`model.go:253-285` | PASS | 画像単独0.060000の専用assertは無いが、数量・単価と合計を照合した |
| AC-2c 精度・丸め | 12000000000000001×0.000000001が12000000.000000001。half_up、half_even、downを境界値で確認し、評価に桁数と規則を固定 | `TestDecimalConsumption`、PG `large_decimal_through_http_and_pg`、`service.go:343-345` | PASS | なし |
| AC-3a 再送・累積 | 同じ操作のHTTP再送で同じ版へ収束。累積100→150は系列の最新値150のみ採用 | PG receipt比較、`cumulative_and_late_children`、`service.go:47-54,135-154,240-262` | PASS | なし |
| AC-3b 親子・二取得元 | 子70・60を含む親200と同一要求gateway200を200として一度採用。原記録の参照・項目採用元を保持。矛盾201と相関不明はreview_needed | PG `cumulative_and_late_children`、独立通常試験の`TestCoverageAdoption`、`coverage.go:26-117` | PASS | なし |
| AC-4 料金版・適用根拠 | P1→P2でも旧評価0.182000と旧料金参照を維持。評価は固定料金版の適用期間・接続・契約・実モデルを照合し、情報源・桁数・丸めを保存 | PG旧評価照合、`service.go:328-353`、API snapshotの旧料金参照 | PASS | なし |
| AC-4 遅着・訂正・再評価 | 訂正と遅着する子の取込みを版追加として保存。旧評価を変更せず現在集計をpendingにし、明示再評価は新IDと前評価参照を保存 | PG訂正・再評価・`cumulative_and_late_children`、`service.go:166-183,314-327`、`query.go:149` | PASS | なし |
| AC-5 四評価種別 | 同一消費のreported、estimated、api_equivalent、allocatedを別系列に保存し、照会は一種類ずつ選択 | PG四種類の結果、`service.go:296-397`、`query.go:139-168` | PASS | なし |
| AC-5 通貨・換算・未設定 | USD/JPYを別bucketへ分け、明示FX150・時点・情報源による36.300000と換算前金額を保持。料金未指定はamount=null、unrated | PG通貨・FX・unpriced、`service.go:328-354,380-395`、`query.go:160-168` | PASS | なし |
| AC-6 複数Scope・Workflow・五帰属 | Scope a/bの和集合でも同じ計測範囲は1件。五帰属をWorkflow付きで集計し、Context membership追加・解除で費用所属が変わらない | PG `attributions_and_missing`・Context所属変更、`query.go:150-151`、`service.go:227-231` | PASS | なし |
| AC-7 案件・料金の非公開 | 共通資源認可を原記録と料金・評価の参照先へ適用。非公開案件・料金とそれを根拠にする金額は個別取得で拒否、集計・exportから除外 | PG非公開fixture、MCP非公開料金fixture、`service.go:264-291`、`access.ReadFilterSQL` | PASS | なし |
| AC-7 Workspace・役割・失効 | 他Workspaceの取得を拒否。閲覧者の保存と失効後のreceipt再送を拒否。編集者・管理者の手動料金設定は許可し、個別契約も通常の共有資源として保存 | PG別Workspace・viewer・失効、MCP別Workspace・現在role、`service.go:56-124,186-189` | PASS | なし |
| AC-8 大量件数・ページ・条件 | 1万件×3版・100Scopeで2件のページと継続を取得。期間・帰属・Workflow・Scope・モデル・評価種別・通貨をサーバーSQLで絞り、タイムゾーン別の日付を生成。GUI要求はlimit=50 | PG `ten_thousand_three_versions`、`query.go:95-168,199-206`、`usage-pricing.tsx:99-119` | PASS | 新たなp95目標は対象外 |
| AC-8 固定位置・追跡・更新範囲 | ページ間のthrough/as_ofを維持。changesでaffected_from/untilと元記録の固定版を返す。訂正時は該当groupのみ更新し、範囲取得は索引を使用 | PG changes継続・構造fixture、`service.go:166-172,191-235,240-262`、`query.go:133-137` | PASS | なし |
| AC-9a 三入口の参照 | HTTPとMCPで利用量・評価・固定料金版・集計・exportの同一結果を比較。GUIも同じHTTP serviceへ接続し、Web/Tauriの集計額0.100000、数量100を画像で確認。Web保存後の版はsnapshotと対応 | MCP `checkUsageMCP`、共通UI、`issue14-web-aggregate.jpg`、`issue14-native-aggregate.jpg`、`issue14-web-api-snapshot.json` | PASS | 検品者によるWeb/Tauri実操作の再実行は無し。渡された画像・応答と実装を照合 |
| AC-9a 手動料金・再送 | GUIのexpected_versionと保持したoperation_id、MCPの明示price-editから共通Putへ到達。HTTP/MCPの再送は同じ固定版に収束し、Webは保存成功画像と新旧版を保持 | MCP再送比較、`usage-pricing.tsx:149-175`、`issue14-web-price-save.jpg`、API snapshot | PASS | なし |
| AC-9b 許可された操作・応答 | read/Context editからprice_setを拒否。none操作のget/queryを拒否・空結果にし、他Workspace・非公開料金と根拠額をMCPへ返さない。書込みschemaは未定義項目を拒否し、業務toolは三つに限定 | MCP実Ory＋SDK、`mcpapi/usage.go:13-51`、共通認可・strict decode経路 | PASS | 任意CLIからの秘密値検出保証は対象外 |
| AC-9b 参照の副作用 | 参照前後のusage_records件数不変。Query/ReadはSELECTのみで、Run開始・発行・評価作成を呼ばない。GUI要求資料のexecution_pathsも空 | PG件数比較、`query.go`、`service.go:264-295`、`issue14-ui-requests.json` | PASS | なし |
| INVAR-1 取込みと実行の分離 | Putの更新先は利用量domainと共通版・receipt・資源認可・変更記録。Run開始や業務変更の再実行への呼出しは無い | `service.go:82-237`、PG再送、API登録差分 | PASS | 未実装の実Run/CLI収集経路は対象外 |
| INVAR-2 原出力・結果と確定の分離 | 利用量の訂正は別domainの版追加・集計更新であり、Runの原出力・実行結果・状態を書き換えない | `service.go:124-237`、`011_usage.sql`、PG訂正履歴 | PASS | 未実装Runとの実接続試験は対象外 |
| INVAR-3 不明値の非補完 | coverage unknownはreview_needed、欠測数量はnull、価格無しはunrated。要求モデルから実モデルを補完せず、報告額・推定額・API換算・配賦を別名で保持 | 独立通常試験、PG、`coverage.go:38-44`、`model.go:118-165,253-285`、`service.go:328-397` | PASS | 欠測理由の保持はB-1として別に未達 |

AC単位ではAC-1がFAIL、AC-2〜9とINVAR-1〜3は上記範囲でPASSである。証拠不足だけでFAILにした条件は無く、INCONCLUSIVEの条件は無い。提供されたGUI観測資料による判定と、検品者が再実行したAPI・DB・MCP試験は区別した。

## 指摘

| 指摘 | 関係する条件 | 到達経路と具体的な影響 | 根拠 | 分類 |
| --- | --- | --- | --- | --- |
| B-1 欠測理由を保存・取得できない | 計画AC-1b、設計書§7.10の「null相当と理由」 | 認可済み編集者が`POST /usage/workspaces/{workspace}/records/{usage-id}`で欠測数量を報告する通常経路。`value`内の数量へ`reason`を付けるとPut内のstrict decodeがErrInvalidを返し、HTTPは400となる。理由を外すとmissing/nullは保存できるが、取得元が示した欠測理由は共有保存・固定版取得から失われる | `usage/http.go:58-76` → `service.go:39-45,93-98` → `model.go:16-47`。PGの`attributions_and_missing`も理由を含めておらず、この未達を検出しない | blocker |

この指摘は固定した受入条件の未達であり、新しい状態分類・自動推測・必須入力制限の要求ではない。原因を補完せず、取得元から与えられた理由を保持してHTTP/PGで照合できることを再確認する。

## 判定

- 総合判定: **FAIL**。blockerはB-1の1件。固定commitの独立検品を終了する。
- 独立実行: `pnpm run test:usage`成功。`TestUsagePostgres`は4.743秒、構造fixtureの初回2件ページは612.1192ms、`usage_group_records`のIndex Scanを観測した。専用Compose `d68089ad164f`はコマンドが片付けた。
- 独立実行: `pnpm run test:mcp`成功。実Ory＋公式SDKの試験は55.77秒、Go packageは57.760秒。料金の三入口共通経路・現在認可に加え、既存の分類・固定版・none/exclude・失効・検索・探索も通過した。専用Compose `de9ea3d1490d`はコマンドが片付けた。
- 独立実行: `pnpm run test`成功。Goのcache表示があったため、同じ固定commitへ`GOFLAGS=-count=1`をそのシェルだけに指定して再実行した。Go全packageとUIの10ファイル35試験が成功し、数値・採用範囲の単体試験も実行された。
- 独立確認: 保存済みWeb集計・料金保存・Tauri集計の画像3枚、API snapshot、要求12件と実行要求0件の資料、全36変更pathの差分を条件へ照合した。試験前の`git diff --check`も成功した。
- 日本語確認: yomiyasuで推敲し、保存後のlintは95/100。箇条書き比率の候補1件は、検品雛形の入力情報・実行結果を並べる用途に合うため維持した。
- 未実行: 検品者によるGUI実操作、check/build/desktop buildの再実行、実CI・merge後照合。GUIについては提供された固定観測資料と共通実装を評価した。他の未実行項目を独立成功とは扱わず、実CI・merge後照合は実装担当の終了手順に残す。
- 標準8080の既存プロセス、本物のGitHub App、秘密鍵は操作していない。検品者による追跡対象の追加は本記録だけで、実装とGitの状態は変更していない。
- 修正後の再確認: B-1の差分、reasonを含む欠測をstrict wire経路で保存・取得する試験、HTTPとPGの一致、null/0/非該当と既存数値計算への影響。修正commitを固定して、この記録に追記する。

## 修正commitの差分再検品

対象は`38f903f46b3e873de2932ca45f04653f3979dfb6`、比較元は初回対象`e0cfddbb8708a7778b4fc38129ffce4c37fcf762`。2026-10-16に同じ独立検品者が確認した。開始時と試験終了時のHEADは対象commit、追跡対象の変更は無かった。コードの探索・確認には再びCodeGraphを先に使った。

製品コードの差分は`Quantity.reason`の任意欄追加と、既知数量と欠測数量の合成時に欠測側の理由を残す処理である。関連単体試験・HTTP/PG fixtureと文書も更新された。理由の推測、必須入力化、新しい制限、認可・計算規則・実行経路の変更は無い。

| 条件・影響範囲 | 確認した経路と証拠 | 再判定 |
| --- | --- | --- |
| AC-1b、B-1 | `TestMissingReasonWire`をcache無効で独立実行し、strict decodeが理由付きmissing/nullを受け付け、再JSON化して`provider_did_not_report`を保持することを確認。PG `attributions_and_missing`も五帰属でHTTP保存・取得・PG payloadを比較し、同じ理由を検証 | PASS、B-1解消 |
| Quantityの伝搬 | `model.go:16-22`の任意欄、`coverage.go:145-151`の合成、既存の構造体コピーによる`disjoint`、評価のQuantitiesへの代入と共通JSON保存を確認。単体試験で既知20＋理由付き欠測がnull・理由付き欠測を保つ | PASS |
| AC-1の他の末端条件、INVAR-3 | 同じPG fixtureが実モデルnull、文字列0、missing/null、not_applicable/nullを引き続き区別。reason未指定の既存fixtureも通過し、欠測や料金なしを0へ補完していない | PASS維持 |
| AC-2・3・4・5・6・7・8 | 全usage単体とPG統合試験を再実行。0.182000・0.242000・36.300000、大きな十進数、丸め、親子・二取得元・累積・再送、履歴・認可・複数Scope・大量件数の既存fixtureが通過 | PASS維持 |
| AC-9、INVAR-1・2 | HTTP保存・取得は同じ共通serviceを使用。今回の差分は認可、MCP tool、GUI、Query/Read、Run・原出力への経路を変更していない。任意欄のJSON伝搬を確認し、初回の三入口・副作用の判定を維持 | PASS維持 |

独立実行した`.tools/go/bin/go.exe test -count=1 -v ./internal/usage`は成功した。`TestMissingReasonWire`、`TestDecimalConsumption`、`TestCoverageAdoption`を実行し、packageは1.124秒。DB試験はこの単体コマンドでは明示的にskipされ、別の`pnpm run test:usage`で実行した。

独立実行した`pnpm run test:usage`も成功した。`TestUsagePostgres`は3.70秒、packageは4.807秒。構造fixtureの初回2件ページは619.2642ms、`usage_group_records`のIndex Scanだった。専用Compose `5bf4942234af`は試験コマンドが片付けた。標準8080と本物のGitHub App・秘密鍵には触れていない。

差分再検品の総合判定は**PASS**。AC-1〜9、INVAR-1〜3は全てPASSで、未解消blockerは0件、INCONCLUSIVEは無い。初回FAILは旧commitの履歴として保持する。今回の差分に認可・MCP・GUIの変更は無く、それらの実操作・実Ory試験は再実行していない。最終headの実CIとマージ後照合は、この独立検品とは別の終了手順として残る。
