# Issue #20の独立検品

## 対象と入力

- 対象commit: `4faefea045bf8443bb002ad61aae6d5cedbd1885`。比較基準main: `7c6b43eaaa273dacbd62de432bf716a74c178ed0`。
- Issue #20、Scope revision 1、区分C。実施日: 2026-10-17。実装者と別の検品担当コンテキストで確認した。
- [計画](2026-10-17-issue-20-plan.md)のAC-1〜10・INVAR-1〜3だけを判定対象とした。工程を編集できるメンバーが人の判断待ちへ回答でき、回答から実行許可を追加しないという承認済み方針を含む。
- 入力はAGENTS.md、AGENTS.local.md、文書案内、開発・Issue手順、設計書の関連節（特に§5.4・§8.7・§10.5）、ADR 0020、工程のrunbook、固定差分、[証拠記録](2026-10-17-issue-20-evidence.md)とそのローカル成果物。実装セッションの会話履歴は継承していない。
- Windowsの既存checkoutで確認した。コード探索はCodeGraphを先に使い、対象の新規workflowファイルも索引から読めた。索引の作成・更新、製品コードの編集は行っていない。
- Windowsアプリの起動、本人の操作、権限設定の変更、実GitHubへの書込みは行っていない。

## 条件ごとの結果

表のPG再実行は `test-results/issue20-independent-workflows.log` を指す。固定の共通検査、単体試験、Run試験、MCP試験はそれぞれ `issue20-check-final.log`、`issue20-unit.log`、`issue20-runs.log`、`issue20-mcp.log`。画面の記録は `.cache/issue20-ui-run.log` と二枚の画像、二つの定義JSONを使用した。

| 条件 | 確認した経路・結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | 項目編集と原文が共通JSONへ反映され、HTTP保存・固定取得・exportと一致する。下書き、検証記録、採用は別記録。大きな整数も保持する | 共通画面、固定JSONとハッシュ一致、`workflows.tsx`・`workflow-json.ts`・`service.go`・`http.go`、PG再実行 | PASS | ファイル選択ダイアログ経由の読込みは未確認。下記参照 |
| AC-2 | 保存・検証・採用にRun作成や外部実行の経路がなく、Run追加0。開始は別APIと明示操作 | `Put`・`Validate`・`Verify`・`Adopt`と`Start`の分離、PG再実行、MCP・画面記録 | PASS | なし |
| AC-3 | 未接続入力、欠けた固定参照・戻り先、能力不足、依存循環、有限予算のない戻り先を拒否し、箇所と理由を返す | `validate.go`・`validate_test.go`・`service.go`、単体試験、PG再実行 | PASS | なし |
| AC-4 | 並列開始・必須合流・三判定・判断待ち・編集者の回答・有限の差し戻しを確認。未回答と未完了の合流で後続なし。再試行は新Run。実行許可取消後も後続なし | `Step`・`readiness`・`Decide`・`beforeStart`、PG再実行の進行・外部条件・予算シナリオ | PASS | なし |
| AC-5 | 明示した出力・派生結果のパスだけをcontext/prompt/commandへ解決し、プレビューと確定inputが一致。未選択出力と会話を暗黙に注入しない | `prepare`・`selectValue`・`Preview`、PG再実行、選択値の単体試験 | PASS | なし |
| AC-6 | 保存だけでは採用不変。明示適用前に影響する未開始ノードと失効する判断を照合し、適用を記録する。開始済み入力と元RunのPASSは保持し、古い入力の遅着判定は工程上STALE。宣言した戻り先だけが新Runを作る | `impact`・`Impact`・`Apply`・`unstarted`・`Step`、PG再実行 | PASS | なし |
| AC-7 | 既存CIのcommit・実行ID・必須ジョブを照合。別commit・未完了・skipはINCONCLUSIVE、失敗はFAIL。観測commandの完了と外部判定を分け、CI起動要求0 | `ciVerdict`・`launch`・`githubobserve.Current`、固定HTTPとPG再実行 | PASS | なし |
| AC-8 | 外部操作は固定対象、必須判断、現在の実行許可、条件付き操作能力と実引数を確認する。割当後に対象が変わるとRunnerの入力取得を拒否。一致時だけ固定接続へ一度書込み | `beforeStart`・`Runs.Assign`・`Runs.Assigned`・`CapabilityGap`、PG再実行の固定HTTP | PASS | 実GitHubへのmerge等は今回の判定方法に含まない |
| AC-9 | GUI/HTTP/MCPが同じserviceを使い、固定定義・検証結果・能力版が一致。現在の権限と参照版を確認し、none・exclude・範囲外・他Workspaceを拒否。参照・検証で実行なし | `mcpapi/workflows.go`・`checkRefs`、実Ory/MCP固定ログ、共通画面、PG再実行。3万Context版後の最新取得も固定MCPログで成功 | PASS | なし |
| AC-10 | 固定実行位置から定義・採用・判断・各試行・Run・原出力をNDJSONへ出す。未取得や失効で完了印を出さない。ネイティブは共通ストリーム保存と固定版完了印を使用する | `export.go`・`run_export.rs`、PG再実行、既存Runの固定ログ、証拠記録のネイティブ局所確認 | PASS | 今回のWindowsアプリ実操作は未実施。共通UIと既存Runnerの証拠を再利用 |
| INVAR-1 | 工程の判断待ち・Runner準備待ちとRunのPauseを分離する。準備待ちでは同じ未開始Runを再照合し、再試行では別Runを作る | `Step`・`launch`・共通画面、PG再実行、既存Runの二Pause試験 | PASS | なし |
| INVAR-2 | 出力は選択されたデータとしてだけ扱う。未定義の開始を勧める出力が定義や実行許可を変更せず、追加実行0 | `prepare`・`Step`、PG再実行の固定出力とRun数8の照合 | PASS | なし |
| INVAR-3 | 条件付き操作の能力・固定commit引数と接続先の保護を維持。CIは読取りだけ。既存の専用worktree・branchと元CLIの経路を再利用する | 起動境界の差分、固定HTTP、既存Run試験、証拠記録のworktree局所試験 | PASS | 元ツールの実操作は既存証拠を再利用 |

## 証拠の限界

ブラウザ拡張の`setFiles`は、ファイルURLへのアクセス許可が無効なため失敗している。ファイル選択ダイアログからの読込み成功とは判定しない。AC-1のデータ往復は、ダウンロードしたJSONを新しいGUI原文下書きへ戻してAPIと照合した証拠で判定した。読込み実装は`file.text()`で同じ`fresh`処理へ渡す経路を確認した。権限設定を変える追加操作は行っていない。

二枚の画面画像と二つの定義JSONのSHA-256は、証拠記録の値と一致した。画面は静止画像として確認し、終了済みの画面fixtureやサービスを再開していない。

## 判定

- 総合判定: PASS。固定条件の未達または今回の差分による合意済み挙動の回帰として、再現入口と影響を示せるblockerは見つからなかった。独立検品を終了する。
- 独立再実行: `pnpm run test:workflows`成功。`TestWorkflowsPostgres`は7.64秒、packageは8.857秒。進行、版適用、STALE、選択入力、外部条件、予算、準備待ち、保存・権限を確認し、専用Compose環境の削除まで終了した。
- 固定ログ: 共通`check`、Go・UIの`test`（UIは12ファイル39件）、Runの専用PG、実Ory/MCPの成功を確認した。固定差分の`git diff --check`も成功した。
- 必須PR CIとマージ後の照合は本検品の対象成果物に含まれず、別途確認が必要。本記録だけではIssueのCloseを判定しない。
- コード変更なし。追加したのは本記録だけで、独立再実行のログはGit管理外の`test-results/`に保存した。
