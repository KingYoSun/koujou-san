# Issue #20の実装と検証

状態: CI待ち。Scope revision 1。[有限な受入計画](2026-10-17-issue-20-plan.md)に対応する実装・局所検証・共通画面の往復を終え、[独立検品](2026-10-17-issue-20-inspection.md)は全条件PASS・blocker 0。PRの最終CIとマージ後照合は未完了。

## 確認済み

- 専用PostgreSQLの`test:workflows`で、版付きJSONの保存・取得・export、CAS・再送、固定採用、未接続入力・能力不足、ページ取得と現在の権限を確認した。保存・検証・採用だけの経路ではRun記録0件。
- 同じ試験で並列・必須合流、FAILからの別Runによる再試行、INCONCLUSIVEの判断待ち、編集者の差し戻し、元の依頼者の実行許可失効、PASS後のcontext・prompt・commandへの選択入力を確認した。未選択の原出力と遷移を勧める文章は入力・追加実行へ混ざらない。
- 定義・Contextを編集しても進行中の採用版を変更せず、明示適用で影響と判断の失効を記録すること、開始済みRunの固定内容を保持すること、工程とRunのexportを確認した。
- 固定HTTPのCI観測と条件付き外部操作で、CI起動要求0、別commit・skip・必須判断不足・割当後の対象変更の拒否を確認した。合致する明示操作だけが固定接続へ一度書き込む。
- 有限の再試行・時間予算、選んだ費用方式による正確な合計、欠測・通貨違いの拒否を確認した。専用PG試験は成功し、所有するCompose環境を削除済み。
- 実Oryと公式MCP SDKによる`test:mcp`は63.366秒で成功。GUIと同じHTTP serviceに対し、定義・静的検証・接続能力の固定版が一致し、none・範囲外・他Workspaceを拒否した。参照・検証によるRunの追加0件。専用Compose環境は削除済み。
- `check:desktop`成功。`test:desktop`は11件成功・既存の実環境専用8件は未実行。Runと工程exportの固定版完了印、既存worktree・DPAPI・入力契約を含む。
- 共通UIの型検査成功。JSON編集と下書き保存の局所試験は7件成功。大きなJSON整数を保った項目編集と、Pause下書きとの分離・Workspace離脱時の削除を確認した。

## 実機確認

今回のためのWindowsアプリ起動・認証・画面終了・スリープ確認は行っていない。OS境界は既存のRun検証と上記の局所試験を使った。本人の手動操作は依頼していない。

専用Ory・DB・共通UIを一環境で起動し、ChromeでRun定義と工程定義の項目編集、保存、検証、採用、固定版のファイル保存、接続能力の取得を確認した。空の新規工程では予算数値を自動で選ばず、試験では並列1・Run上限2を本人のデータと分けた合成入力として指定した。GUI→API→ファイル→新しいGUI原文下書き→APIで同じJSONを照合し、大きな整数も保持した。判断待ちの結果条件と戻り先も同じ固定版に保存・採用された。Run追加0件。画面fixtureは756.19秒で完了し、一時タブ・所有するCompose環境と検証用OAuth情報を片付けた。

ブラウザ拡張の`setFiles`は「Allow access to file URLs」が無効なため失敗した。拡張の権限設定は変更していない。ファイル選択ダイアログ経由の読込み自体は未確認で、保存したJSON内容を原文エディターへ戻してデータの往復を確認した。Windows側は一覧取得の一度目がtimeout、再試行は成功したが、ファイル選択ダイアログはなく、Windows画面の入力は行っていない。

画面証跡は`test-results/issue20-definition-ui.jpg`（SHA-256 `6fc311a43167ac0d21899518365df5c5f5db1245e50575b7c1203ebc82ab84c7`）、`test-results/issue20-workflow-ui.jpg`（`bed2c9e6541777ac89564e535f021c2e8497c24a6d969e40bcc315f9fb801b25`）。取得した定義JSONは`issue20-run-definition.json`（`6d9b8c498aeb8f82ca83a8c7c476e46a685d423dd6a90c8e422182af4b288632`）、`issue20-workflow-definition.json`（`b66f547bd507e3dd83246727b3efc247393615dd3e05547ffe80cdcddb683fae`）として同じディレクトリに保存した。

## 条件と証拠

| 条件 | 判定に使う証拠 |
| --- | --- |
| AC-1 | 上記の共通GUI・API・ファイル往復、定義の固定版と採用版の区別、PGのCASと不変な採用 |
| AC-2 | 保存・検証・採用のPG経路と共通画面でRun追加0、観測接続への書込み0 |
| AC-3 | 不正な固定定義の箇所付き検証、欠けた固定参照、未接続入力、能力不足、存在しない戻り先、依存循環・無限再試行の拒否 |
| AC-4 | PGの並列・必須合流・三判定・編集者の回答・別Runへの差し戻し、実行許可取消、回数・時間・費用予算 |
| AC-5 | 出力と結果の明示パス選択、context/prompt/commandのプレビュー・確定入力一致、UTF-8の大きな整数 |
| AC-6 | 編集のみで採用不変、明示適用の影響と人の判断の失効、古い入力で遅着したPASSのSTALE化と定義した戻り先による新Run |
| AC-7 | 固定HTTPの既存CI観測、別commit・必須ジョブのskip/失敗、command終了と外部結果の分離、CI起動0 |
| AC-8 | 必須判断不足・現在の実行許可・割当後の対象変更を起動境界で照合し、一致する条件付き操作だけ一度実行 |
| AC-9 | 実Ory・MCP SDK・HTTP・GUIの固定定義/能力、none・範囲外・別Workspaceの拒否、副作用0 |
| AC-10 | 固定工程と各Runのexport、採用変更・判断・元の出力・入力の対応、ネイティブの完了印と既存CLI/Git継続の証拠 |
| INVAR-1〜3 | Runner準備待ちとPauseの分離、再試行の新Runと旧判定保持、出力内の指示による追加実行0、条件付き操作と既存worktree試験 |

## 修正と最終確認

画面で見つけた採用リクエストの余分なWorkspace属性を取り除いた。1万Context×3版の後に定義の旧版を返した不具合は、履歴位置の文字列順序を数値列の順序へ修正した。最終MCP試験は60.89秒で成功し、同じ3万版の後の更新・最新取得を検証した。共通の`check`と`test`、RunのPG試験も成功。UI試験は12ファイル39件成功。追加したSTALE・遅着結果・準備待ち・欠けた固定参照のPG試験も成功した。残る全体確認はPR CIで行う。

固定commit `4faefea045bf8443bb002ad61aae6d5cedbd1885`の独立検品は全AC/INVARをPASS、blocker 0件と判定した。独立のPG再実行も成功（package 8.857秒）。その後のローカル`test:database`も成功し、新規migrationを含む保存・停止・復元を確認した。既存migration 001〜015の差分は0。

## 調査

共通JSONと既存の永続化・Run契約を使う方式は[ADR 0020](../adr/0020-versioned-workflows.md)へ記録した。`.codegraph/`を使って既存Run・権限・Context・GitHub観測・exportを調べた。新しいworkflowファイルが索引にない場合は、未登録を確認してから直接読んだ。索引の作成・更新は行っていない。AGENTS.mdで参照するDESIGN.mdとADR 0014は現行checkoutに存在せず、既存の共通画面構成に沿って実装した。
