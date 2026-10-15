# Issue #12の独立検品

## 固定した対象

- 対象commit: `22dc95ccd16bdc3a7268f4e8664dd796ccd9ba72`。基準main: `4d3f16dc42dfa85d2432e82067b4f1768b553f40`。
- [Issue #12](https://github.com/KingYoSun/koujou-san/issues/12)、Scope revision 1、区分C。既存下書きの移行と旧版アプリとの混在不可はユーザー承認済み。
- 検品者: 実装セッションの履歴を継承しない別コンテキストのCodex（`issue12_independent_review`）、2026-10-16。
- 入力: Issue本文、AGENTS、開発・Issue実装手順、[計画](2026-10-16-issue-12-plan.md)、[検証記録](2026-10-16-issue-12-evidence.md)、[ADR 0011](../adr/0011-context-record-cache.md)、設計書と固定commitの差分。実装者の自己評価だけで判定せず、経路と試験を照合した。
- 対象外: Run実行、CLI/CIの開始、外部更新、LLM操作。未依頼の利用場面を追加しない。

## 条件ごとの判定

| 条件 | 確認した経路と根拠 | 判定 |
| --- | --- | --- |
| AC-1 | 共通編集画面→SyncEngine→Context API。四粒度、順序、固定参照、履歴、参照版の明示採用を実PG試験・操作記録・採用JSONと照合 | PASS |
| AC-2 | Groupingはmembershipだけを保存。本文や子の所属を変えず、直接所属と所属文書からの参照を区別 | PASS |
| AC-3 | 固定参照を再確認して既存resolverへ渡す。none、重複、exclude、異なる版、抜粋、由来、添付をfixtureと照合 | PASS |
| AC-4 | 空選択を未確定とし、取得不足・権限・停止・遅延・打切りの理由を表示。状態表示試験と実操作記録を照合 | PASS |
| AC-5 | 保存・取得・resolveにRun/CLI/CI/外部更新の呼出しはない。要求集計のexecution_pathsは空、実DB更新もContext関連のみ | PASS |
| AC-6 | 下書き保持、同じ要求IDの再送、競合比較、明示再適用、失効時削除を独立試験で確認。二クライアントの操作証跡と一致 | PASS |
| INVAR-1 | Zustandは画面状態、TanStack Queryは共有表示、SyncEngineは基準版・下書き・未確認要求を担当。共有版はAPIで確定 | PASS |
| INVAR-2 | Scopeの選択・所属変更からincludeへ自動追加しない | PASS |
| INVAR-3 | WebのContext操作へネイティブCLI実行経路を追加していない | PASS |
| INVAR-4 | 認証・Workspace選択から編集画面へ進め、端末登録やハーネスを要求しない | PASS |
| INVAR-5 | 保存・所属変更・固定版選択・プレビューと実行開始を分離 | PASS |
| 規模の前提 | 候補・履歴・Scope・下書きはページ取得、同期は対象のwatch、一件編集は対象レコードとヘッダーのみ。1万件移行試験、要求集計、Scopeの実行計画を照合 | PASS |
| 保存形式の移行 | IndexedDB/Windows SQLiteで基準版・pending・競合・receiptを保持。失敗後の再試行、境界分離、失効時削除を確認 | PASS |

## 実行結果と限界

独立実行した `pnpm run check`、`test`、`build`、`check:desktop`、`test:desktop`、`test:sync`、`test:contexts`、`test:search` は全て成功した。共通UIは34件成功、desktopは3件成功・対話環境用の既存5件ignore。各実DB試験の専用Composeは終了時に削除された。

Web/Tauriの実操作は再実施せず、提供された4画像、操作記録、要求集計、DB証跡を現コードと照合した。`git diff --check`はpsql生出力の証跡2ファイルの末尾空白・EOF空行のみを報告した。固定条件の未達には当たらない。実装・修正・commit・GitHub書込みは検品者が行っていない。検品終了時のHEADは対象commitで、追跡対象の変更はなかった。

総合判定は **PASS、blocker 0件**。固定条件を全て判定したため検品を終了する。PRのCIとマージ後照合は後続工程であり、この時点では未確認。

この記録は検品者の報告から実装担当が作成し、yomiyasuの意味保持・主述対応・時制の観点で推敲した。検品後の差分は本記録と現在状態・計画の進捗更新だけであり、コードや条件は変更していない。
