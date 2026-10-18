# Issue #30: 区分C独立検品

## 対象と入力

- 対象commit: `8ec7b2637de7d8e0604d48ee705a44f017635dd1`。基準main: `c7ac1c40c3d8d45a8d1731c0e3931a0c9ba3c3f8`。
- Scope revision: 1。独立検品者: Codexの別コンテキスト。実施日: 2026-10-18。
- 入力: `AGENTS.md`、`AGENTS.local.md`、文書案内、開発手順、Issue lifecycle、固定したIssue本文 `.cache/issue30-body.md`、計画、ADR 0030・0026、設計書§9.6・9.7・12.4、関連Run/工程/Context選択の手順、実装とテスト、実装者から渡された `test-results/issue30-*`。
- 実装者の会話履歴は継承していない。初回の画面試験が能力応答のMeta欠落で保留となり、対象commitで修正されたという観測情報だけを受け取った。
- Windows、PowerShell、所有する試験専用PostgreSQLで確認した。実モデルと外部GitHubへの操作、既存サービスの停止は行っていない。
- コード探索はCodeGraphを先に使用した。`DESIGN.md` と `docs/adr/0014-uiux-dev-flow.md` は存在しなかった。

## 条件ごとの結果

初回判定。修正版の最終画面証拠は到着していない。API部分が通っていても、必須の画面操作が未観測の条件は未確認とした。

| 条件 | 確認した経路・操作と証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- |
| AC-1 | `checkDefinitions` の二CLI・二モデル接続から共通 `Put → Validate → Verify → Adopt → CreateRun`。独立再実行した `pnpm run test:clientops` がPASS。通常Runは一件、未割当・開始要求なし。既存証拠は `issue30-explicit-run1.log`。 | PASS | 実CLIの起動はこの条件の対象外。 |
| AC-2 | 未設定の依頼は操作の `question` に保持され、回答に固定能力の参照を追加して定義版へ到達する。原応答・質問・回答は共通レコードへ保存する。`checkDefinitions` とclientops独立再実行はPASS。 | PASS | なし。 |
| AC-3 | 自動/項目別採用は同じ保存・検証を使う。しかし部分生成案を手動編集へ戻す入口で既存の未変更値を失う。下記C30-1。 | FAIL | 修正後の手動修正・不採用・再生成を含む画面証拠。 |
| AC-4 | 他メンバーの元版更新はCASで拒否し、固定項目を変更する案はproposalとして保持する。項目別採用はPASS。生成中の画面編集は `inputSignature` で反映を抑止する実装を確認。 | 未確認 | 最終画面で下書きを保持した観測。C30-1の手動編集経路も再確認する。 |
| AC-5 | 生成・回答・採用後のRun/Run.pause/工程実行は0件。明示したCreateRunだけでRunが一件となる。モデルHostへ書込み・開始toolを公開せず、MCPサーバーでも拒否する。clientops独立再実行と `issue30-mcp1.log` がPASS。 | PASS | なし。 |
| AC-6 | 二モデル接続の定義・操作usage、同一要求のreceipt再送をclientopsで確認。既存MCP統合証拠とソースでmetadata限定、none、禁止分類、Scope、再接続、本人による引継ぎを照合。Host独立再実行は10試験PASS。 | 未確認 | 修正版の共通画面で二モデル接続の生成・修正・採用を完了した証拠。 |
| INVAR-1 | 定義保存/採用から `runs.Action(request-start)` への呼出しはない。CreateRunは固定採用版から新しいinputを作る。既存開始済みinputの更新経路は追加されていない。既存workflow進行証拠 `issue30-workflows.log` はPASS。 | PASS | なし。 |
| INVAR-2 | 自動/項目別採用は同一transaction上の既存workflowsサービス、`run-definition-v1`、現在権限、前提版、共通検証/採用を使う。手動保存と既存workflowの回帰証拠はPASS。 | PASS | なし。 |
| INVAR-3 | 質問/回答は `client-proposal` / `client-answer` と操作状態で扱う。Run.pauseへの変換はなく、fixtureでも0件。 | PASS | なし。 |

## 指摘

### C30-1: 部分生成案を手動編集へ戻すと未変更の定義項目を失う

- 分類: blocker。AC-3の未達。AC-4の手動編集へ戻す経路にも関係する。
- 到達経路: 既存のRun定義で提案方式を選び、名称だけの変更などを依頼する。Hostの指示どおり、モデルが `definition_utf8` に `{"name":"変更後"}` のような変更項目だけを返す。「この案を下書きで編集」を押すと、`client-selection.tsx:765` がこのJSONをそのまま `onDraft` へ渡し、`workflows.tsx:874` のコールバックが既存下書きの `text` を置換して `fields` を空にする。
- 影響: command、schema、端末、既存prompt、手動固定値など、省略した項目が下書きから消える。`compose` は新しい `draft.text` に編集fieldを適用するだけで、元定義とは合成しない。名称だけを手修正して通常保存してもschemaがなく拒否される。schemaを含む部分案でも、保存後の共通検証ではspec/command不足になる。利用者が変更していない既存設定を再入力しないと、手動修正から通常の定義採用へ進めない。
- 根拠: ADR 0030と `client-selection-host.ts:155` は省略項目の維持を約束する。サーバーの `adoptDefinition` は `mergeDefinition(base, proposed)` でこれを実現しているが、上記画面の手動編集経路には合成がない。`workflows.tsx:54` のcompose、`:306` のsave、`workflows/service.go:280` のschema検査、`workflows/validate.go:22` のspec検査を照合した。
- 必要な再確認: 既存commandと未送信の固定値を持つ定義に部分変更案を出し、手動編集へ戻して未変更値を保持したまま保存・検証・採用できること。実モデルは不要。

## 判定と実行した検証

- 初回総合判定: FAIL。blockerはC30-1の一件。最終UIの未確認事項も残る。
- `pnpm run test:clientops`: 独立再実行PASS。試験専用PostgreSQLだけを作成・終了した。`TestClientOperationsPostgres` は3.58秒、パッケージは4.451秒。
- `pnpm --filter @koujou-san/client test -- client-selection-host.test.ts workflow-json.test.ts`: 独立再実行PASS、2ファイル10試験。
- 既存MCP統合・workflow進行・静的検査のログと、その試験の対象ソースを照合した。全CIや実モデル試験を独立再実行したという判定ではない。
- 今回の指摘は固定した手動修正経路に限定する。新しい利用場面・未依頼のエッジケースは追加していない。
- コード変更・commitは行っていない。修正commitと最終UI証拠を受領後、影響する条件を再判定する。
