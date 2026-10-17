# Issue #21: 固定成果物の独立検品

## 対象と入力

- 対象commit: `fec855c62631fa68e14265bf131ef910a637597d`。
- 基準main: `fbb0637ff4f9de825a37a4dc808f7495517f6758`。
- Issue #21、Scope revision 1、区分C。条件は[受入計画](2026-10-17-issue-21-plan.md)のAC-1〜8・INVAR-1〜3に固定した。
- 検品者: 実装会話を継承しない別コンテキストの独立検品担当。実施日: 2026-10-17。
- 入力: AGENTS.md、AGENTS.local.md、文書案内、開発・Issue・検品手順、設計書第10章と関連するRun・権限の仕様、ADR 0021、固定commitの実装・試験、公開可能なログと画面画像。
- 実装者の会話履歴は継承していない。証跡文書の成功宣言・自己評価をそのまま判定根拠とせず、対応するコード・試験のassertion・ログ・画像と照合した。秘密を含み得る`*-private.json`と認証情報は読んでいない。
- 環境: 同じWindowsホストと既存開発環境。HEADと対象commitの一致、検品開始時の差分なしを確認した。コードの探索・読取りには既存CodeGraphを先に使った。索引の作成・更新はしていない。

## 証拠と確認方法

| 記号 | 証拠 | 確認した内容 |
| --- | --- | --- |
| E1 | `test-results/issue21-independent-inspections.log` | 独立して`pnpm run test:inspections`を実行し成功。専用PG、認証済みの二登録端末、固定入力、否定例、Stale、差分、例外判断、失効、参照権限、ページ取得を確認 |
| E2 | `test-results/issue21-independent-contract.log` | 独立して`go test ./internal/inspections -run 'TestStructuredReport\|TestDeclarations' -count=1 -v`を実行し成功。形式不正・未確認欄欠落・未指定条件・証拠不一致・宣言と強制の区別を確認 |
| E3 | `test-results/issue21-native-handoff.log`と`runner/execution.rs`の固定CLI受渡し試験 | 成功ログと試験本体を照合。二つのサーバー割当を別保存域の独立cloneへ渡し、実Node CLIの観測入力を固定inputと比較。元checkout、受渡し先のcommit/status、合成の認証・実装履歴・未commitファイルの不在をassertionで確認 |
| E4 | `test-results/issue21-inspection-ui.jpg`、`.cache/issue21-ui-run.log`と`inspections.tsx` | 保存画像を目視し、過去のFAIL、Stale、明示none、宣言・強制・未確認、条件別証拠、現在のexpiredを確認。実Ory試験の成功・終了ログとUI fixture、保存・採用・比較・例外判断の入口を照合 |
| E5 | `test-results/issue21-cli-probe.log`、インストール済みCLIのversion/help | Codex 0.162.1、Claude Code 2.1.300とADRの指定flagを照合。公開集計の両終了0・合成マーカー一致を確認。秘密診断の詳細まで確認したとは扱わない |
| E6 | `test-results/issue21-runs.log`、`issue21-workflows.log`、`issue21-check-final.log`、`issue21-unit.log`、`issue21-desktop-unit.log` | 共通Run・工程・静的検査・既存試験の成功ログを照合。今回の共通hookは通常Runで元の結果を返し、既存の起動前検証を維持することをコードで確認 |

E2のコマンドは`apps/server`からroot配下の`.tools/go/bin/go.exe`で実行した。E3〜E6の実接続・UI・回帰試験は検品中に再実行せず、保存済みの結果と固定commitの試験・実装を照合した。E1・E2はこの検品で取得した独立した実行結果である。

## 条件ごとの結果

| 条件 | 確認した経路・操作と結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | 定義→採用→`prepare`→Context Preview→Manifest→共通Run.input→CLI入力。fresh/noneはContextなし、contract/selectは選択した固定Contextだけを注入。実入力とCLI観測が一致し、実装履歴の合成マーカーは渡らない | E1、E3。`launch.go:24`、`postgres_test.go:218`、`execution.rs:492` | PASS | 固定条件内ではなし |
| AC-2 | device-a/device-bの割当、独立clone、別保存域、同じ固定commit、元checkout不変を照合。共有側の入力は固定成果物・定義・Manifestを持ち、programの実pathや資格情報は端末のAdapterに残る | E1、E3。`postgres_test.go:86`、`execution.rs:242`、`execution.rs:492` | PASS | 物理的に別のPCでの実行は未確認。固定条件の二登録端末・別保存域は同じWindowsホストで確認 |
| AC-3 | 能力文字列は宣言まで。明示した機器ログでobservedを記録し、session=enforcedの不足は開始拒否。未知のpermissionsはunobserved。開始・割当後の取得・startedでも現行能力、権限、採用基準を再確認する | E1、E2、E4、E5。`contract.go:78`、`results.go:56`、`results.go:90`、共通RunのBeforeStart呼出し | PASS | OS全体・認証・モデル側記憶の完全分離は未確認で、強制済みとは表示しない |
| AC-4 | 共通Runの終了処理→派生assessment→条件・証拠照合。正常例はPASS。終了0のFAIL、自然文の不正形式、必須欠落、未実行、証拠不足はPASSにならず、原レポートを保持。明示した判定fieldの投影も原出力を変更しない | E1、E2。`contract.go:112`、`contract.go:135`、`results.go:212`、`runs/service.go:671` | PASS | 外部証拠の主張自体を自動で真と証明するものではない |
| AC-5 | 対象digest、条件、必要Contextを各一回変更して採用し、旧RunをそれぞれSTALEとして取得。下書き保存だけでは採用は変わらず、旧inputと当時の結果は不変。新しい検品は別Runとpreviousで関連付く | E1。`postgres_test.go:267`、`results.go:36`、`launch.go:100` | PASS | 固定条件内ではなし |
| AC-6 | 元Run・変更範囲だけの差分案件はINCONCLUSIVE。影響確認・固定証拠を加えた案件はPASSかつpreviousを保持。元の条件・判定と証拠の利用可否も照合する | E1、E2。`postgres_test.go:327`、`contract.go:190`、`results.go:250` | PASS | 影響範囲の自動的な真偽証明は行わない |
| AC-7 | 四種類の証拠由来を別値として保存。同じ固定対象のPASSとFAILを保持。編集者の例外判断は別記録で、元FAILと実行許可は不変。viewerと別Workspaceの主体は拒否。UIも比較と別の判断保存を持つ | E1、E4。`postgres_test.go:347`、`results.go:341`、`inspections.tsx:599` | PASS | 固定条件内ではなし |
| AC-8 | evidenceのavailable→expiredを新しい版で保存し、Currentは現在のexpiredを表示。当時のRun・判定はbyte比較で不変。画面でも失効と過去判定を分けて表示 | E1、E4。`postgres_test.go:372`、`results.go:291`、`inspections.tsx:308` | PASS | 外部所在の自動巡回検証は今回の条件外 |
| INVAR-1 | 未指定条件・異なるManifest digest・別条件または対象を覆わない証拠をPASSにしない。保存済みの条件と対象に照合する | E1、E2。`contract.go:138`、`contract.go:142` | PASS | 固定対象・条件の外側への適合は判定していない |
| INVAR-2 | 未定義の開始・実行許可を勧めるレポートでも追加Run数は0。レポートと例外判断を保存しても工程定義・権限は更新しない。共通の工程検証は維持 | E1、E6。`postgres_test.go:195`、`results.go:341`、`service.go:29` | PASS | 固定条件内ではなし |
| INVAR-3 | 製品哲学Contextを明示選択したinputには含み、noneのinputには含まない。全Workspaceや実装会話の収集経路は追加されていない | E1、E3。`launch.go:83`、`postgres_test.go:232`、`postgres_test.go:297` | PASS | 固定条件内ではなし |

通常の一覧は最大100件の索引付きメタデータ、履歴は選択したownerと固定throughによるページで取得する。新migration 017と`http.go:31`、E1のメタデータ・履歴ページのassertionを照合し、全Context・全原出力を通常操作で取得する実装にはなっていないことを確認した。

## 指摘と判定

- 総合判定: **PASS**。AC-1〜8・INVAR-1〜3をすべて判定し、固定条件の未達または今回による合意済み挙動の回帰に当たるblockerはなかった。
- コード・fixtureは編集していない。変更はこの検品記録とGit管理外の独立試験ログのみ。マージとIssue状態の変更は行っていない。
- 認証情報の読取り・再認証、GUI起動、実スリープ、実モデルへの再依頼は行っていない。保存済みの実接続・画面証拠を再利用し、CLIの分離能力を確認した範囲より広く主張していない。
- DESIGN.mdとADR 0014は対象checkoutに存在しなかったため、共通画面は固定仕様、既存画面構成、保存画像とコードで照合した。これは固定条件の未判定を生んでいない。
- PR CIとマージ後照合はこの固定commitの検品とは別であり、この記録では完了したと判定しない。

固定した条件の判定を終え、blockerがないため、この独立検品を終了する。日本語はyomiyasuで意味・条件・断定の強さを保って推敲した。

## 不正レポート表示の差分検品

- 対象commit: `9d3ab14eeca40e855b77bd5767793c9c95f41cff`。比較元: `2d5253392e05e055b1f181f6738d51cfa404f273`。
- 初回検品では、保存済みの不正レポートを表示する経路の`criteria_results.map is not a function`を見落としていた。AC-4のこの表示経路について、初回の判定を今回の差分判定で置き換える。
- 差分は`inspections.tsx`、追加した描画試験、証跡文書の3件だけと確認した。backend、native、CSS、要件は変更されていない。
- 修正前の失敗ログと、固定コードの`acceptedReport`、`InspectionResult`、`showRun`を照合した。不正形式では条件表の行を生成せず、形式・未判定と原データを表示する。元のraw reportは詳細表示に保持する。正常な条件・証拠の表示構造は維持されている。
- 人の判断案は同じ`acceptedReport`を使うため、不正形式では条件IDが空配列となり、正常時は元の条件IDを使う。理由・未確認・元結果の参照、判断を明示保存する操作は変わっていない。
- 独立して`apps/client`から`pnpm exec vitest run src/inspections.test.tsx`を実行し、不正形式と正常表示の2件が成功した（`test-results/issue21-independent-invalid-ui.log`）。実装側の修正前後ログ、最終UI試験13ファイル41件、check・buildの成功ログも照合した。GUI、DB、nativeの試験は再実行していない。
- 差分判定: **PASS**。修正後のAC-4、正常表示、人の判断案の初期化にblockerはない。他のAC・INVARは変更の影響がないため、既存のPASSを再利用する。`9d3ab14e`で全固定条件PASS・blocker 0件と判定し、差分検品を終了する。
