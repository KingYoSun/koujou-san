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

最初の共通画面では、MCPへ未接続の要求を同じ操作で再開し、端末/CLI不足の質問を保持した。その後、能力toolの応答に完了状態の情報がなくHostが `incomplete_candidates` で保留する不具合を確認した。metadataの共通応答形式へ修正し、認証付き試験にも完了状態の照合を追加した。修正後の実Ory/MCP試験もPASS（`test-results/issue30-ui2.log`）。

## 最終の共通画面

Chrome、日本語、通常のviewport・既存theme、`https://localhost:54724`。`test-results/issue30-definition-ui.json` は所有モデルHTTPの受信記録とDB状態、`issue30-ui-domain.json` は各操作と定義版、`issue30-ui-created-run.json` と `issue30-ui-fixed-input.json` は作成したRunと固定入力。

1. 端末を未指定にした依頼から質問を表示。端末とCodex接続を回答すると、固定能力をMCPで読んで同じ操作を再開し、確認操作を挟まず保存・採用した。
2. 同じ定義でモデルαからβへ切り替え、Claude接続へ変更した。手動の名称を保持して自動採用した。通常の明示作成で `run:c8e48372-0da5-4b21-9858-34ca56b950f6` が一件でき、状態queued、割当・開始要求なし。
3. 提案方式で名称と接続先の元値・生成案を開き、名称の採用checkboxを外して採用した。名称は維持され、同じ定義の新しい版となった。
4. 部分案を下書きへ戻す経路は、独立検品C30-1で省略値の消失を指摘された。原文fragmentを使う部分マージへ修正し、元の名称・prompt・巨大整数を保持する同条件の試験をFAIL→PASSで確認（`issue30-partial-draft-before.log` / `after.log`）。
5. 生成開始直後の画面編集でも、送信準備後に比較基準を取ってしまう時間差を検出した。比較基準を依頼操作の最初で固定し、定義取得後にも照合する修正を行った。同じ画面操作で修正前は名称が戻ったが、修正後は「受入: 生成中に編集した未送信名」を保持し、「編集中の下書きは変更されているため、現在版を取得して比較してください」と表示した。その部分案を下書きへ戻しても未送信名・Claude接続・promptを保持した。

採用後も手動固定のpathを端末の定義下書きへ保持する。最終のHost/精度保持11試験と共通checkは `issue30-host-final2.log` / `issue30-check-final.log`。独立検品の修正差分確認、必須CI、merge後照合は継続中。

## 必須Contextの省略

最後の照合で、definition-policyの必須Contextを指定しても、モデルがselection全体を省略するとその必須参照が採用定義から消えるAC-6の未達を実PGで確認した（`issue30-required-before2.log`）。生成案の省略時にも既存の選択処理を通し、必須・除外を維持するよう修正した。正規化後にも手動固定pathを照合する。`issue30-required-final.log` の実PG試験と `issue30-domain-final.log` の関連Go試験はPASS。Go vetも成功した。新しい利用場面や制限は追加していない。
