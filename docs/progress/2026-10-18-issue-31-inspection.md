# Issue #31: 固定成果物の独立検品

## 対象と入力

- 対象commit: `b4ecf61170e8113c6b9a5346d1038e13a9d6b7de`。
- 基準commit: `3804bab8a8c4f769a4fb64ce844a7a6185c5c1fe`。
- Issue: [#31](https://github.com/KingYoSun/koujou-san/issues/31)、区分C、Scope revision 1。
- 検品者・実施日: 実装セッションの会話履歴を継承しない別コンテキストのCodex、2026-10-18。
- 入力: `AGENTS.md`、`AGENTS.local.md`、文書案内、開発手順、Issueの実装手順、Issue #31本文、[計画](2026-10-18-issue-31-plan.md)、[実装証跡](2026-10-18-issue-31-evidence.md)、[ADR 0031](../adr/0031-output-context-generation.md)、固定差分、関連する共通Context・MCP・Hostの実装と試験、`test-results/issue31-*`。
- 対象: 固定出力の指定箇所から四粒度と別の所属を生成し、通常のContextへ保存・編集・再利用する経路。手動・提案・自動、二つの所有モデルHTTP、明示有効化後の新しい確定出力、再送・担当終了・503・累積上限・手動復旧、原記録の保持を確認した。
- 対象外: 実モデルの本番API、新しい認証、外部への変更、意味の類似だけによる統合・削除、全Contextの事前列挙、開始済みRunへの自動伝播。検品を理由に新しい利用条件を追加していない。
- 環境: Windows / PowerShell。開始時のHEADと作業ツリーが指定commitに一致することを確認した。コード探索は既存のCodeGraphを先に使った。GitHub本文は`gh issue view 31`で読んだ。

これは固定実装と既存観測結果の検品であり、実装や試験実行の代行ではない。実装側のPASS表だけを根拠にせず、試験のassertion、送信・保存の経路、生ログを照合した。

## 条件ごとの結果

以下のソース位置は対象commitの位置を指す。ログの共通ディレクトリは`test-results/`。

| 条件 | 確認した経路・操作 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | `generationSource`が元Run/outputの固定版・JSON pathを検証し、`adoptGeneration`が子から親へ順序付き参照を解決する。Originのoutput版・digest・箇所・操作IDはサーバーが付け、版別の由来へ参照Contextを保存する。所属は別配列・別記録となる。原出力への更新経路はない。 | `generation.go:147`、`generation_apply.go:64`・`:211`・`:228`・`:248`、`generation_test.go:151`以降、`issue31-core-final.log`、`issue31-context-ui.json`の四粒度のorigins | PASS | 本番モデルは対象外 |
| AC-2 | 新規targetの版0と既存固定版を区別し、構成のchildrenと所属変更を通常`contexts.Service.Put`へ渡す。未選択の必要な子は拒否し、元版競合では一括rollbackする。明示したContextだけ新版になり、他の参照元へ伝播しない。保存結果は通常Context画面と通常の固定版選択・基準版解決を共用する。画面の全チェック解除は送信前に拒否される。 | `generation_apply.go:122`以降、`contexts/service.go:190`以降、`client-selection.tsx:434`、`generation_test.go:207`以降・`:285`以降・`:309`以降、`issue31-baseline-conflict.log`、`issue31-ui1.log`、UI JSONの同じ`generation-ui-shared`の複数版 | PASS | 修正後の連続生成を旧UIの実行結果とは扱わない |
| AC-3 | observed / inference / judgment / proposalを保存し、生成案と由来で別表示する。自己申告は元出力に残り、独立検品判定へ書き込まれない。再利用・重複・矛盾は候補として記録し、削除や自動統合を行わない。現在の更新権限を通常Context検証で確認し、過去判定を保持することと新しい基準版の明示採用を表示する。 | `generation_apply.go:68`・`:101`、`context-generation-proposal.tsx:46`以降、`context-generation-origin.tsx:75`以降、通常Contextの認可、指定箇所以外の自己申告を含めないMCP試験、`issue31-mcp1.log` | PASS | 新しい独立検品の実行は本生成操作の範囲外 |
| AC-4 | 二モデル・手動・提案・自動が同じ操作履歴とContext保存を使う。未送信の下書き固定値は初期メッセージへ含めず、採用時に固定値の復元・一致検証を行う。元版と現在権限の不一致は保存を止める。原応答と案を先に保持し、自動採用の失敗はsavepointを戻して案を残す。 | `client-selection-host.ts:171`以降、`generation_apply.go:150`〜`:198`、`results.go:60`〜`:150`、`generation_test.go:237`以降、`issue31-revision1.log`、`issue31-failure-recovery.log`、UI JSONのowned-alpha / owned-betaと共有Contextの履歴 | PASS | 既存DB本文の読取りは通常の許可範囲に従う。フィールド単位の新しい秘匿要件は置かない |
| AC-5 | 方針保存と有効化を分離し、Run終了を受理する経路だけが新しい待ち行列を作る。方針版/output版から操作IDを固定し、通知・受領・反映応答の再送を照合する。送信意図は保存済みで、担当終了後の結果不明を再送しない。503・累積要求上限からモデル要求なしの手動案へ切り替えられる。Context更新は生成契機にならず、操作上限は再開でリセットしない。 | `generation_rules.go:98`・`:166`、`generation_queue.go:23`以降、`operations.go:363`以降、共通Hostの再送照合、`generation_test.go:327`〜`:441`、`issue31-auto1.log`、`issue31-backfill1.log`、`issue31-core-final.log`、`issue31-failure-recovery.log` | PASS | 本番モデルの障害は所有fixtureで代替 |
| AC-6 | 生成の開始・応答・採用はclientopsの操作記録、Contextと別の所属記録だけを作る。Run / command / Pause / Workflowノードの作成を呼ばず、usageの帰属は`operation`の操作IDとなる。 | `operations.go:68`以降、`generation_apply.go`、`results.go:164`以降、`generation_test.go:274`以降・`:446`、`issue31-core-final.log` | PASS | なし |
| AC-7 | 開始済みRunのinputと過去Manifestをbyte単位で比較する。生成したContextは旧基準版では選択不可で、明示更新した基準版からのみ選べる。生成Contextはrun-output由来を持ち、output直接参照の引継ぎ経路と区別される。 | `generation_test.go:65`〜`:86`・`:207`〜`:233`、`contexts/service.go:476`以降、`generation_apply.go:229`、`run-output-record.tsx`、`issue31-baseline-conflict.log`、`issue31-core-final.log` | PASS | なし |
| INVAR-1 | 文書構成はUnitのchildren、所属はMembership、注入は通常のSelection / Baseline / Manifestで確定する。生成側に別の注入経路はない。 | `generation_apply.go`、通常Contextサービス、AC-2 / AC-7の証拠 | PASS | なし |
| INVAR-2 | 生成の保存先に原output・inspection判定・過去Manifestが含まれない。原出力の自己申告を保持するassertionと過去input/Manifestのbyte比較がある。 | `generation_test.go:207`以降・`:442`、`generation_apply.go`、`issue31-core-final.log` | PASS | なし |
| INVAR-3 | 操作の現在方針・現在権限・指定範囲を開始前、MCP取得時、配信前、採用時に再確認する。run_getは指定outputの箇所だけを返し、元inputや他チャンネルを返さない。noneでContext toolを拒否し、元出力の依存参照からScope外のContext本文権限を広げない。モデルへの編集・Run開始toolもない。 | `generation.go:236`、`operations.go:119`以降、`clientops/mcp.go`、`mcpapi/service.go:142`以降、`context_generation_test.go:70`以降、`issue31-mcp1.log`、UI JSONの認証header/cookie非送信と受信側の禁止本文検証 | PASS | 本番モデル、新認証は対象外 |

## 証拠の区別と制約

- `issue31-generation-before.log`は、生成方針を保存できない変更前のFAILを記録している。変更後の成功は`issue31-core-final.log`などの実PGログで確認した。
- `issue31-context-ui.json`と`issue31-ui1.log`は、実Ory・MCP SDK・Chrome・二つの所有モデルHTTPによる観測である。二つの元出力、一つの生成待ち、四粒度の由来、共有Contextの複数版、自動操作のappliedが残っている。
- このUI観測には修正前の連続生成で保留になった操作も残る。新規Context IDを生成操作ごとに決める最終修正は、このUI serverには含まれていない。その修正の判定には、同じ結果へ別の生成を行う`generation_test.go:183`以降と`issue31-core-final.log`のPASSを使った。UI JSONを修正後の再実操作の証拠とはしていない。
- `issue31-check-final.log`の型・静的・format検査、`issue31-test-final.log`のGo全packageとUI 18ファイル68試験、`issue31-failure-recovery.log`の実PG統合PASSを読んだ。本検品ではこれらを再実行していない。
- 全Contextの本文・IDを先に配列化する経路は追加されていない。対象通知は索引で方針を照合し、出力・過去分・結果・由来は固定参照とページを使う。既存の構成解決は明示対象の子だけを扱う。
- `DESIGN.md`と`docs/adr/0014-uiux-dev-flow.md`は指定checkoutに存在しない。今回の固定条件は計画・ADR 0031・Issue本文と既存共通画面で照合し、欠落文書から追加条件を推測していない。
- 検品中に実装担当から固定値の意図を確認した。対象は未送信の下書き値の暗黙送信防止であり、権限内の既存DB本文の読取り禁止ではない。初期メッセージのDTOと保存時の検証をこの範囲で判定した。

## 指摘と判定

- 総合判定: PASS。AC-1〜7、INVAR-1〜3の全条件を判定した。
- blocker: なし。固定条件の未達または今回の既存回帰として具体的な到達経路を示せる指摘はない。
- 必要な実装修正: なし。
- 文書の正確化: 検品中に実装担当がADRの「手動固定した値はモデルへ送らず」を、未送信下書きの初期メッセージ非送信と通常権限内の共有版取得を区別する表現へ修正した。この一文だけの差分を確認し、実装変更や追加要件がないことを確認した。
- 検品で実行した確認: 固定commit・基準差分・関連コード・GitHub本文・生証跡の照合、`git diff --check 3804bab b4ecf61`。差分検査はPASS。
- 未実行: 本番API、新認証、追加のUI実操作、試験の再実行、PRの必須CI、配布更新・復元CI、merge後照合。CIとmerge後照合は実装担当が別に確認する。本記録のPASSだけでIssue全体をCompleteとはしていない。

検品記録はyomiyasuの指針で推敲し、固定条件・観測結果・未確認事項の意味を保って点検した。リンターの指摘は箇条書き比率だけであり、対象・証拠・未確認事項を区別する役割があるため、その構造を維持した。
