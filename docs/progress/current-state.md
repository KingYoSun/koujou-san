# 現在状態

更新日: 2026-10-15（JST）

現在の到達点は実装開始用の開発環境です。製品設計は [設計書](../../koujou-san-design-doc.md)、構成判断は [ADR 0001](../adr/0001-development-foundation.md)、環境構築の証跡は [2026-10-15の記録](2026-10-15-development-environment.md) を参照してください。

## 今回の受入条件

| 条件 | 期待結果 | 確認方法 |
| --- | --- | --- |
| AC-ENV-1 | Issueや直接の指示から実装を開始できる | 指示書、手順、Issue／PR様式の参照と記述を照合 |
| AC-ENV-2 | 固定依存を導入して検証できる | setup／doctor／check／test／buildとnative検証 |
| AC-ENV-3 | 共通UIとGo APIの起動を確認できる | healthとWebの応答、nativeの起動確認 |
| AC-ENV-4 | 標準サービスの開発用構成が起動する | Compose設定、migration、health・接続確認 |
| INVAR-ENV-1 | 既存の設計書とkukuriを変更しない | Git差分・対象pathの確認 |
| INVAR-ENV-2 | 個人設定・秘密値・既存権限を複製しない | ignoreと新規.env生成、設定の確認 |

## 次の実装

設計書§14の段階Aから、API契約、認証・Workspace参加、PostgreSQLの業務schema、Bleve検索、ArcadeDB投影、MCP、共有状態の観測・同期をIssueへ分ける。Hopper・Triage・利用量の基盤を段階Aの計画から落とさない。段階BでRunnerと一巡の制御、段階CでLLM操作・編集・交換を接続する。

これらの製品機能とログイン画面は未実装です。最初のIssueでは対象と受入条件を固定し、設計書§18の対応する未決事項を判断します。
