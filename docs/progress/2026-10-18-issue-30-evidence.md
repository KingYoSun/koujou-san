# Issue #30: Run定義生成の検証

状態: In progress。条件は[計画](2026-10-18-issue-30-plan.md)のAC-1〜6・INVAR-1〜3。全体の現在判定は[現在状態](current-state.md)へ集約する。

## 実装と検証範囲

[ADR 0030](../adr/0030-run-definition-generation.md)の共通Host・操作履歴・定義保存を実装した。実在するCLIの読取りで確認したCodex 0.162.1とClaude Code 2.1.300の能力を、所有fixtureの二つの論理接続へ登録した。fixtureはCLIを実行せず、二つのモデルHTTP接続を使う。本Issueの条件は統合fixtureによる確認であり、新しい有料モデル要求や認証は行っていない。ネイティブの搬送は変更せず、#26の実モデル・DPAPIと#29の起動検証を再利用する。

## 証拠

- `test-results/issue30-clientops-before.log`: 既存clientopsの変更前試験はPASS。
- `test-results/issue30-definition-before.log`: 変更前はRun未作成でのdefinition目的を拒否するため、新条件の試験はFAIL。
- `test-results/issue30-explicit-run1.log`: 実PGで生成・通常の検証/採用・原応答/usage・質問回答・未送信の固定値・他編集者の元版変更・項目別採用・要求再送を確認。生成/採用後のRun/Pause/工程実行は0件。明示した作成でRunが一件だけでき、割当と開始要求は未設定。PASS。
- `test-results/issue30-workflows.log`: 同一transactionへ接続した共通定義の既存進行・外部条件・版管理・参照・手動保存の回帰試験はPASS。
- `test-results/issue30-final-host.log`: 共通Hostと精度保持の10試験はPASS。definition/noneでmetadataの三toolだけを渡し、Context取得を要求した不正なモデル応答を保留する。
- `test-results/issue30-mcp1.log`: 実Ory・公式MCP SDK・PG・Bleve・ArcadeDB。新目的のmetadata読取り、未許可toolと分類の拒否、none、再接続、本人の別クライアントへの引継ぎを確認。10,000 Context×3版でも同じ境界を確認。PASS。
- `test-results/issue30-check.log`: Go vet・型・formatの共通検査はPASS。

最初の共通画面では、MCPへ未接続の要求を同じ操作で再開し、端末/CLI不足の質問を保持した。その後、能力toolの応答に完了状態の情報がなくHostが `incomplete_candidates` で保留する不具合を確認した。metadataの共通応答形式へ修正し、認証付き試験にも完了状態の照合を追加した。最終画面、独立検品、必須CI、merge後照合は継続中。
