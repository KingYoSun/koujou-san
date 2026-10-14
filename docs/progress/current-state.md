# 現在状態

更新日: 2026-10-15（JST）

開発環境の構築を終え、[親Issue #2](https://github.com/KingYoSun/koujou-san/issues/2)の実装を#3から番号順に進めている。製品設計は[設計書](../../koujou-san-design-doc.md)、環境構築の証跡は[2026-10-15の記録](2026-10-15-development-environment.md)を参照。

## 環境構築の受入条件

| 条件        | 期待結果                               | 確認方法                                      |
| ----------- | -------------------------------------- | --------------------------------------------- |
| AC-ENV-1    | Issueや直接の指示から実装を開始できる  | 指示書、手順、Issue／PR様式の参照と記述を照合 |
| AC-ENV-2    | 固定依存を導入して検証できる           | setup／doctor／check／test／buildとnative検証 |
| AC-ENV-3    | 共通UIとGo APIの起動を確認できる       | healthとWebの応答、nativeの起動確認           |
| AC-ENV-4    | 標準サービスの開発用構成が起動する     | Compose設定、migration、health・接続確認      |
| INVAR-ENV-1 | 既存の設計書とkukuriを変更しない       | Git差分・対象pathの確認                       |
| INVAR-ENV-2 | 個人設定・秘密値・既存権限を複製しない | ignoreと新規.env生成、設定の確認              |

## Issue #3の判定

状態: In progress。Scope revision 1を維持。契約と境界は[ADR 0002](../adr/0002-shared-api-contract.md)、対象と条件は[計画](2026-10-15-issue-3-plan.md)、CLI・native試作の観測結果は[検証記録](2026-10-15-issue-3-evidence.md)に置く。

| 条件       | 現在の結果                                                                                                                          | 残作業                         |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| AC-1〜AC-3 | 共通契約、Go/TypeScript fixture、UIとnativeの境界を実装し、局所テストと独立検品を通過                                               | PRのCIとマージ後の照合         |
| AC-4       | 担当表を作成。独立検品で見つかった#29の重複を修正し、再検品を通過                                                                   | CIとマージ後の照合             |
| AC-5       | WindowsデスクトップとWSL内CLIを採用。WindowsのCodex/Claude Code、WSL入出力、Tauri callback・PKCE・DPAPI保存を確認し、独立検品を通過 | 最新commitのCIとマージ後の照合 |

#3の判断待ちは解消した。全条件、独立検品、必須CI、マージ後の照合が揃ったらCloseし、#4へ進む。#4〜#34は未着手。業務API、ログイン、Context、Runner、検索・探索、LLM操作は未実装であり、段階Aや初期リリースの完成とは扱わない。
