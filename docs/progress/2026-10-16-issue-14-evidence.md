# Issue #14: 利用量・料金・費用評価の検証

- 対象: [実装計画](2026-10-16-issue-14-plan.md)のAC-1〜9・INVAR-1〜3、Scope revision 1。
- 方針: 個別契約単価もWorkspace共有を初期値とし、履歴を無期限に保持する。中央の接続先一覧を必須にしない。[ADR 0013](../adr/0013-usage-pricing.md)、[操作手順](../runbooks/usage-pricing.md)を参照する。
- 環境: Windows 11、Go 1.27.2、Node.js 22、pnpm 10.16.1、専用ComposeのPostgreSQL・Ory・ArcadeDB、Bleve、Chrome拡張、Tauri。

## 条件と結果

| 条件 | 確認した経路と結果 | 証拠 |
| --- | --- | --- |
| AC-1 | 四帰属と未帰属、実モデルnull、理由付き欠測・0・非該当をHTTPで保存・取得し、PGのpayloadと比較 | `usage/postgres_test.go`の`attributions_and_missing`、`TestMissingReasonWire` |
| AC-2 | cache込み入力100と別記入力80+20は0.182000。出力に含まれるreasoningを再加算しない。画像2で0.242000。12000000000000001×0.000000001はHTTP/PGでも12000000.000000001 | `usage/model_test.go`、`large_decimal_through_http_and_pg` |
| AC-3 | 同じ操作の再送は同じ版。累積100→150は150。子70・60を含む親200と同じ要求のgateway200は200。矛盾・不明範囲は確認対象 | `TestCoverageAdoption`、`cumulative_and_late_children`、PG試験のreceipt照合 |
| AC-4 | P1→P2の料金変更、明示再評価、利用量訂正と再評価で旧評価・旧料金を保持。訂正後の現在集計は再評価待ち | PG履歴試験、[共通UIとAPIの保存結果](assets/issue14-web-api-snapshot.json) |
| AC-5 | 四評価種別を分け、USDとJPYを合算しない。架空FX150で36.300000。料金なしは未算定。換算根拠と換算前の金額を保持 | PG試験の評価・二通貨・FX・未設定の照合 |
| AC-6 | Scope a/bの和集合で同じ計測範囲は1件。Contextの所属を追加・解除しても費用集計が変わらない | PG試験の所属変更、五帰属の集計 |
| AC-7 | 非公開案件・料金とその金額、別Workspaceを個別・集計・exportで拒否/除外。閲覧者の書込みと失効後のreceipt再送を拒否 | PG試験、実Ory+公式SDKの`checkUsageMCP` |
| AC-8 | 1万件×3版・100Scopeで上限付きのページ、照会条件・権限・反映位置・集計時点を固定した継続、遅着・訂正の元記録と対象時刻を確認 | `ten_thousand_three_versions`、`changes`の継続試験 |
| AC-9 | GUI/HTTP/MCPで原記録・料金版・評価・集計・exportを参照。手動料金をWebから更新し、HTTP/MCPの再送は同じ履歴へ収束。read/Context editから料金変更を拒否 | `checkUsageMCP`、下記のWeb/Tauri確認 |
| INVAR-1〜3 | 参照で記録数が増えず、実行・発行経路を呼ばない。取込みは利用量domainだけを更新し、原出力を変更しない。欠測・確認対象を0や実請求額へ補完しない | Go試験、[UI要求記録](assets/issue14-ui-requests.json)、実装経路 |

## 共通UIの実操作

専用fixture `63a3a8351ea4`で実Ory認証、1万Context×3版・100Scope、架空の個別契約単価を使用した。Chrome拡張で利用量・料金・評価・期間集計・exportを参照し、既存の料金版を読み込んで情報源を更新した。料金版は2版となり、既存評価は初版の根拠と0.100000を維持した。

初回の集計操作で、共通HTTP helperへ渡すbodyを二重にJSON化していたため取得が失敗した。同じ操作を修正後に再実行し、0.100000と数量100を表示できた。Windowsのreleaseアプリも専用APIへ接続し、同じ推定額・通貨・数量を確認した。API文字列から生成した未使用のDPAPI保存先だけを使い、標準APIの認証情報は変更していない。検証後にアプリとWebタブを閉じ、専用fixtureとそのOAuthファイルを片付けた。

- [Webの期間集計](assets/issue14-web-aggregate.jpg)
- [Webの料金版保存](assets/issue14-web-price-save.jpg)
- [Windowsアプリの期間集計](assets/issue14-native-aggregate.jpg)
- [料金の新旧版・旧評価・exportのAPI結果](assets/issue14-web-api-snapshot.json)
- [Webからの要求](assets/issue14-ui-requests.json): 利用量・料金への要求12件、実行・発行への要求0件。nativeは同じHTTPS APIへ直接接続した。

## 実行した検証

`pnpm run test:usage`で専用PGの保存・認可・履歴・集計を確認した。1万件×3版・100Scopeの初回の2件ページは633.9426ms、計測範囲の取得は`usage_group_records`のIndex Scanだった。これは一回の構造確認であり、p95や新しい性能SLAを達成したという意味ではない。

`pnpm run test:mcp`は実Oryと公式SDKでPASS（55.40秒）。手動料金の固定版・HTTP/MCPの再送、通常のContext編集との分離、none境界、非公開料金を根拠にする金額、別Workspace、現在の役割を確認した。既存の分類・Manifest・失効・検索・探索もPASS。UI fixtureは580.64秒で正常終了した。

rootの`check / test / build`、`check:desktop / test:desktop / build:desktop`を実行しPASS。共通UIは35試験、通常のdesktop試験は4件PASS・実環境を要する5件は既定でignored。専用の`live_context_ui_prepare`は明示実行してPASS。Cargoの既存のPDB出力名衝突・linker警告は残るがbuildは成功した。

## 残る判定

初回の独立検品でAC-1bの欠測理由を保持する欄が不足していた。`reason=provider_did_not_report`を送る`TestMissingReasonWire`は修正前にFAILし、任意の理由欄を追加した後はPASSした。理由を含む欠測と既知数量の合成、HTTP保存・取得・PG比較も確認した。取得元が示した理由を保持し、原因をサーバーで推測しない。

固定commit `38f903f46b3e873de2932ca45f04653f3979dfb6`の[差分再検品](2026-10-16-issue-14-inspection.md)で全AC・INVARがPASS、blockerとINCONCLUSIVEは0件となった。最終headの実CIとマージ後のtree照合が揃うまでは#14をCloseしない。実CLIの収集は#24、本格的な費用比較画面は#32の対象であり、本記録で実装済みとは扱わない。

## マージ後の照合

PR #46の最終headは`d24675c5e5e430a4e30abe5261e7082608c6b8e2`。[CI 38863532381](https://github.com/KingYoSun/koujou-san/actions/runs/38863532381)でLinuxが4分13秒、Windowsが5分24秒、GitGuardianが成功した。独立検品対象の`38f903f`からの差分は検品・証跡・現在状態の3文書のみである。

マージしたmainは`cdf6c14ad6a97c50db1e385a3714c84fc3102f29`。最終headとmainのtreeは`f2fc48613f25e5c55a3312d19eac5366185e00aa`で一致し、`git diff --exit-code d24675c5e5e430a4e30abe5261e7082608c6b8e2 HEAD --`は差分0で成功した。全AC・INVAR・独立検品・実CI・マージ後照合を終え、Issue #14をCloseした。
