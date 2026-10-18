# Issue #30: 区分C独立検品

最新対象は `6d3156e532be8dd63b1fccb3faec438d2a69e99d`。最終判定はPASS、AC-1〜6・INVAR-1〜3を満たし、未解消のblockerと条件上の未確認事項はない。末尾に追加差分の確認を記録する。

## 対象と入力

- 最終対象commit: `6d3156e532be8dd63b1fccb3faec438d2a69e99d`。画面差分検品対象: `9b9e46da62f31b1296bff08bc03163361181adc4`。初回対象: `8ec7b2637de7d8e0604d48ee705a44f017635dd1`。基準main: `c7ac1c40c3d8d45a8d1731c0e3931a0c9ba3c3f8`。
- Scope revision: 1。独立検品者: Codexの別コンテキスト。実施日: 2026-10-18。
- 入力: `AGENTS.md`、`AGENTS.local.md`、文書案内、開発手順、Issue lifecycle、固定したIssue本文 `.cache/issue30-body.md`、計画、ADR 0030・0026、設計書§9.6・9.7・12.4、関連Run/工程/Context選択の手順、実装とテスト、実装者から渡された `test-results/issue30-*`。
- 実装者の会話履歴は継承していない。初回の画面試験が能力応答のMeta欠落で保留となり、対象commitで修正されたという観測情報だけを受け取った。
- Windows、PowerShell、所有する試験専用PostgreSQLで確認した。実モデルと外部GitHubへの操作、既存サービスの停止は行っていない。
- コード探索はCodeGraphを先に使用した。`DESIGN.md` と `docs/adr/0014-uiux-dev-flow.md` は存在しなかった。

## 条件ごとの結果

最終候補の差分と追加証拠を照合した結果を示す。画面操作は実装担当によるChrome実操作の観測記録を受領し、HTTP受信・DBの固定証拠とコードを独立に照合した。検品者による画面の再操作は行っていない。

| 条件 | 確認した経路・操作と証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- |
| AC-1 | `checkDefinitions` の二CLI・二モデル接続から共通 `Put → Validate → Verify → Adopt → CreateRun`。独立再実行した `pnpm run test:clientops` がPASS。通常Runは一件、未割当・開始要求なし。既存証拠は `issue30-explicit-run1.log`。 | PASS | 実CLIの起動はこの条件の対象外。 |
| AC-2 | 未設定の依頼は操作の `question` に保持され、回答に固定能力の参照を追加して定義版へ到達する。原応答・質問・回答は共通レコードへ保存する。`checkDefinitions` とclientops独立再実行はPASS。 | PASS | なし。 |
| AC-3 | 自動/項目別採用は同じ保存・検証を使う。最終画面で元値/案を比較し、名称を非採用として同じ定義の次版へ採用した。部分案の手動編集復帰はC30-1修正で省略値を保持する。`issue30-ui-domain.json`、画面観測記録、部分案試験のFAIL→PASSと独立再実行11試験を照合。 | PASS | なし。 |
| AC-4 | 他メンバーの元版更新はCASで拒否し、固定項目を変更する案はproposalとして保持する。最終画面では再生成後も固定名を保持した。生成開始直後に編集した未送信名は画面に残り、比較案内を表示する。同じ部分案を下書きへ戻しても名称/Claude接続/promptを保持した。開始時のsignature固定と定義取得後の再照合をコードで確認。 | PASS | なし。 |
| AC-5 | 生成・回答・採用後のRun/Run.pause/工程実行は0件。明示したCreateRunだけでRunが一件となる。モデルHostへ書込み・開始toolを公開せず、MCPサーバーでも拒否する。clientops独立再実行と `issue30-mcp1.log` がPASS。 | PASS | なし。 |
| AC-6 | 二モデル接続の定義・操作usage、同一要求のreceipt再送をclientopsで確認。最終画面ではα/βで同じ定義の生成/変更/採用に到達した。`issue30-definition-ui.json` の二モデル受信記録、`issue30-ui-domain.json` の同じ定義IDと版、修正後MCP統合PASSを照合。metadata限定、none、禁止分類、Scope、再接続、本人による引継ぎを維持する。 | PASS | なし。 |
| INVAR-1 | 定義保存/採用から `runs.Action(request-start)` への呼出しはない。CreateRunは固定採用版から新しいinputを作る。既存開始済みinputの更新経路は追加されていない。既存workflow進行証拠 `issue30-workflows.log` はPASS。 | PASS | なし。 |
| INVAR-2 | 自動/項目別採用は同一transaction上の既存workflowsサービス、`run-definition-v1`、現在権限、前提版、共通検証/採用を使う。手動保存と既存workflowの回帰証拠はPASS。 | PASS | なし。 |
| INVAR-3 | 質問/回答は `client-proposal` / `client-answer` と操作状態で扱う。Run.pauseへの変換はなく、fixtureでも0件。 | PASS | なし。 |

## 指摘

### C30-1: 部分生成案を手動編集へ戻すと未変更の定義項目を失う

- 初回分類: blocker。AC-3の未達。AC-4の手動編集へ戻す経路にも関係する。最終候補では解消済み。
- 到達経路: 既存のRun定義で提案方式を選び、名称だけの変更などを依頼する。Hostの指示どおり、モデルが `definition_utf8` に `{"name":"変更後"}` のような変更項目だけを返す。「この案を下書きで編集」を押すと、`client-selection.tsx:765` がこのJSONをそのまま `onDraft` へ渡し、`workflows.tsx:874` のコールバックが既存下書きの `text` を置換して `fields` を空にする。
- 影響: command、schema、端末、既存prompt、手動固定値など、省略した項目が下書きから消える。`compose` は新しい `draft.text` に編集fieldを適用するだけで、元定義とは合成しない。名称だけを手修正して通常保存してもschemaがなく拒否される。schemaを含む部分案でも、保存後の共通検証ではspec/command不足になる。利用者が変更していない既存設定を再入力しないと、手動修正から通常の定義採用へ進めない。
- 根拠: ADR 0030と `client-selection-host.ts:155` は省略項目の維持を約束する。サーバーの `adoptDefinition` は `mergeDefinition(base, proposed)` でこれを実現しているが、上記画面の手動編集経路には合成がない。`workflows.tsx:54` のcompose、`:306` のsave、`workflows/service.go:280` のschema検査、`workflows/validate.go:22` のspec検査を照合した。
- 再確認結果: `workflow-json.ts:53` の `mergeFields` は既存objectの省略項目を維持し、変更する値だけをJSON原文fragmentで置換する。`workflows.tsx:878` は現在の下書きと合成してから手動編集へ戻す。名称/prompt/巨大整数を残してtargetだけ変える同条件の失敗・成功ログと、最終画面の未送信名/Claude接続/prompt保持を照合した。独立再実行もPASS。新しいblockerはない。

### 最終画面で検出された生成中の下書き上書き

実装担当は、開始直後の編集を送信準備後の比較基準へ取り込んでしまうAC-4未達を観測し、同じ時間差操作で修正を確認した。`start` と回答/再開操作は非同期処理の前にsignatureを固定する。`onApplied` は定義取得後の `isCurrent()` 照合を通った場合だけ画面状態を更新し、その更新を下書き保存のawaitより前に行う。最終画面では「受入: 生成中に編集した未送信名」と比較案内を保持した。固定pathも次の下書きへ残る。修正差分と観測が一致し、AC-4の未確認を解消した。

## 判定と実行した検証

- 最終総合判定: PASS。AC-1〜6・INVAR-1〜3はすべてPASS。未解消のblockerと条件上の未確認事項はない。初回対象ではC30-1によりFAIL、AC-4/6は画面証拠待ちだった。
- `pnpm run test:clientops`: 独立再実行PASS。試験専用PostgreSQLだけを作成・終了した。`TestClientOperationsPostgres` は3.58秒、パッケージは4.451秒。
- `pnpm --filter @koujou-san/client test -- client-selection-host.test.ts workflow-json.test.ts`: 初回は2ファイル10試験、最終候補では2ファイル11試験を独立再実行してPASS。
- 最終の共通checkは `issue30-check-final.log`、修正後の実Ory/MCP統合PASSは `issue30-ui2.log` で確認した。`issue30-ui-created-run.json` はqueued・割当/開始要求なし、`issue30-ui-fixed-input.json` は採用版/能力/検証/採用の参照とClaudeへの固定入力を保持する。`issue30-ui-domain.json` では明示作成したRunとinputはそれぞれ一版で、後の定義変更によって増えていない。
- 既存MCP統合・workflow進行・静的検査のログと、その試験の対象ソースを照合した。全CIや実モデル試験を独立再実行したという判定ではない。
- 今回の指摘は固定した手動修正経路に限定する。新しい利用場面・未依頼のエッジケースは追加していない。
- コード変更・commitは行っていない。今回は検品記録だけを更新した。必須CIとmerge後の内容照合は実装担当の終了判定に残るもので、本検品のPASSが代替するものではない。

## 必須Context保持の追加差分

この節は中間候補 `697ac2265b5b0b641a64b2aee88a73c68abbad1e` の検品履歴。比較元は `9b9e46da62f31b1296bff08bc03163361181adc4`。画面コードと搬送は変わらず、前回の画面証拠を再利用した。

| 対象条件 | 差分の照合と証拠 | 判定 |
| --- | --- | --- |
| AC-6 必須/除外 | selectionを省略してもpolicyのInclude/Excludeがあれば共通 `selection → resolveSelection` を通す。必須参照を落とす失敗は `issue30-required-before2.log`、保持した採用版は `issue30-required-final.log`。独立に `pnpm run test:clientops` を再実行しPASS（3.45秒）。共通selectionは必須順序と除外を維持し、候補証拠・現在権限・解決可否の確認も省略しない。 | PASS |
| AC-3 承認済みの自動反映 | 固定path照合を `raw(d)` 後へ移した結果、未送信の `inputs: []` を変更していない案まで自動採用を拒否する。下記C30-2。 | FAIL |
| AC-4 固定項目保持 | 正規化後に固定値を変えて採用する経路は拒否される。ただしC30-2の過剰な拒否が残る。 | PASS |
| AC-1/2/5、INVAR-1〜3 | 既存生成/質問/通常Run作成/副作用境界は統合再実行PASS。保存/能力/版管理は共通の既存処理を維持する。ほかの変更はない。 | PASS |

### C30-2: 未変更の空配列を固定すると自動採用が止まる

- 初回分類: blocker。今回差分によるAC-3の回帰。最終候補では解消済み。
- 到達経路: 端末内下書きの `inputs: []` を手動固定する。方針に `fixed_paths: ["/inputs"]` と `fixed_values: {"/inputs":"[]"}` を保存し、モデルは固定項目を省略した有効な定義案を返す。採用処理のbaseには固定値の空配列があるが、`Definition.Inputs` の `omitempty` により `raw(d)` では当該fieldがなくなる。`definitions.go:261` の比較が存在の不一致を検出し、自動反映せずproposalへ戻す。
- 影響: 利用者もモデルも固定値を変更していないのに、承認済みの範囲の自動反映が止まる。同じ処理で `/required` などの省略可能な空配列も影響する。
- 独立再現: trackedコードを変更せず、一時ファイルとGoの `-overlay` で既存manual-snapshot fixtureの固定値に `/inputs: []` だけを追加した。実PGで `definitions_test.go:124: unsent fixed field was lost`、`TestClientOperationsPostgres` はFAIL（2.36秒）。試験専用DBと一時overlayは終了・削除した。
- 必要な再確認: 既存の固定field比較を保ったまま、Context選択の正規化による変更も検出すること。未送信の空配列を固定して省略した案が自動採用される同条件を確認する。

中間候補の総合判定はFAILだった。元の必須Context消失は解消したが、C30-2が残っていた。既存の未送信固定値と承認済み自動反映への差分の影響として、次の候補で再確認した。

## 空配列回帰の修正と最終判定

対象は `697ac2265b5b0b641a64b2aee88a73c68abbad1e` から `6d3156e532be8dd63b1fccb3faec438d2a69e99d` への差分。固定fieldの原文比較を構造体変換前へ戻し、共通選択処理で変わる `/selection` だけを正規化前後の同じ構造体表現でも比較する。

| 対象条件 | 最終確認 | 判定 |
| --- | --- | --- |
| AC-3 | 未送信の `inputs: []` と `required: []` を固定し、モデルが省略した有効な案を自動採用できる。`issue30-empty-fixed-before.log` のFAIL、`after.log` のPASSと、同fixtureの独立再実行PASSを確認。C30-2は解消。 | PASS |
| AC-4 | 元値と生成案の固定field比較は変換前に行うため、空配列の省略による誤拒否を避けつつ固定値の変更を拒否する。`/selection` が共通処理で変わった場合も、固定されていれば後段で拒否する。既存固定名・項目別採用・元版競合試験もPASS。 | PASS |
| AC-6 | selection省略時のpolicy必須参照の反映は維持する。共通selection/resolveの必須順序・除外・候補証拠・現在権限の照合は変わらず、必須参照保持の実PG試験もPASS。 | PASS |

最終候補で `pnpm run test:clientops` を独立再実行しPASS。`TestClientOperationsPostgres` は3.61秒、パッケージは4.517秒。所有する試験専用DBだけを作成・終了した。画面・搬送の差分はなく、前回の画面証拠と11試験の結果を再利用する。AC-1/2/5・INVAR-1〜3の経路も変更されていない。

最終総合判定はPASS。全AC・INVARを満たし、C30-1/C30-2は解消済み。未解消のblockerと条件上の未確認事項はない。コード変更・commitは行わず、この検品記録だけを更新した。必須CIとmerge後の内容照合は引き続き実装担当が確認する。
