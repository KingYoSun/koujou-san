# Issue #6の独立検品

## 対象と入力

- 固定commit: `809a7d7a8d624759af2a9e8cf1c8dd078a6a099f`。基準: `0eeee997d5365ad9db1b7af35452102b2f3b12fa`。
- Issue #6、Scope revision 1、区分C。検品担当: `issue6_independent_review`、2026-10-15。
- 実装者の会話履歴を継承しない別コンテキストへ、Issue、計画、ADR 0005、AC・INVAR、固定差分、検証結果と実画面観測を渡した。
- 実装、権限・秘密値・再送・保存経路を読み取り専用で確認。自動テストは再実行せず、実装担当が提供した実PG・実Ory・Windows結果を照合した。
- Context実装、検索、Run割当、CLI起動停止は今回の対象外。

## 条件ごとの結果

| 条件 | 確認経路と証拠 | 判定 |
| --- | --- | --- |
| AC-1 | 作成・非所属・招待受諾・取消・期限・再送とWeb/Tauri実画面。hashchange修正を追加照合 | PASS |
| AC-2 | Workspace・Resource・Grantを使う共通Check。対象差替え・包含からの非継承・対象未指定の拒否 | PASS |
| AC-3 | 役割と独立した実行許可。管理者・閲覧者・端末初期不許可を実画面と照合 | PASS |
| AC-4 | Device主体・許可Workspace・担当対象・input/report制限。User偽装・管理操作を拒否 | PASS |
| AC-5 | ロック後の現行認可、取消・離脱・旧世代・再送拒否。接続表示はCLI停止を主張しない | PASS |
| AC-6 | Windows DPAPI保存・再生成接続・更新・失効。共有DTO・journal・receiptに秘密値なし | PASS |
| INVAR-1〜4 | GUI/HTTPがサーバーの共通認可へ接続し、後続MCPへ同じCheckを提供。権限取消と停止、資格情報と共有表示を分離 | PASS |

## 判定

総合判定は適合、blockerなし。汎用native APIはprivate発行応答の資格情報をUIへ返さず、BFF更新はCSRFを検証する。固定差分の`git diff --check`は成功した。

検品完了時点で最新desktop buildは実行中だったため、その成功を検品者は主張していない。実装担当が後から成功を確認し、[検証記録](2026-10-15-issue-6-evidence.md)へ記録した。PR CIとマージ後照合はこの検品の後に行う。
