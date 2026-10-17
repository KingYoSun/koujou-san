# Issue #22 独立検品

## 対象と入力

- 対象commit: `07a928cc62b2397430274e05a8e2f38d704dcc56`。
- 基準main: `b8af5a2421c531a3bd83e3ec0818c3789f67b498`。
- 対象: Issue #22、Scope revision 1、区分C。[計画](2026-10-17-issue-22-plan.md)のAC-1〜7とINVAR-1〜3を変更せず判定した。
- 検品者: 実装会話を継承しない別担当 `issue22_independent_review`。実施日: 2026-10-17。
- 環境: Windows、PowerShell、rootのpnpm入口、試験専用PostgreSQL、固定GitHub HTTP。新しいApp作成、本人再認証、GUI起動、OS実機操作は実施していない。

`AGENTS.md`、`AGENTS.local.md`、文書案内、開発・Issueライフサイクル・検品・外部起票の各runbook、設計書§11.5/11.7/12.1〜12.6、ADR 0022、固定commitの計画・実装・試験を入力とした。実装者の[観測記録](2026-10-17-issue-22-evidence.md)、指定されたログと画面画像は、提供された観測結果として照合した。実装者の総合評価を判定根拠に置き換えていない。

作業開始時のHEADは対象commitと一致し、追跡ファイルの差分はなかった。`.codegraph/`が存在するため、コードの理解はCodeGraphのexplore/nodeから始めた。秘密鍵と`.cache/*private*.json`は読んでいない。`DESIGN.md`と`docs/adr/0014-uiux-dev-flow.md`は存在しなかったため、画面の確認は固定ACと起票runbookへの照合に限定した。

Hopperの条件評価・生成・予算・連鎖、PR/merge等への書込み拡張、外部サービスの能力を超える一回限り作成の保証、新たな実行方式は対象外とした。

## 証拠

| 記号 | 確認した内容 |
| --- | --- |
| E1 | 検品担当が`pnpm run test:publications`を独立再実行。`TestPublicationsPostgres`成功、package 2.328秒、終了コード0。ログ: `test-results/issue22-independent-publications.log`。 |
| E2 | 固定commitの`publications/{service,process,remote,http,model}.go`、`postgres_test.go`、`runs/connector.go`、共通Run/Storeとの差分、GitHub Appの鍵読取り・token交換、migration 018、API起動接続、共通UIとそのfixture。 |
| E3 | 提供された共通画面の操作記録、`test-results/issue22-publication-ui.jpg`、`.cache/issue22-ui-run.log`。画像は`fixture/repo`の外部作成確認・同じ要求とRun・本文・取得時刻を示す。ログは実Oryを含む試験の成功とAPI停止中の接続拒否を含む。端末復元を含む操作の全過程を検品担当が再操作した証拠ではない。 |
| E4 | 提供ログ`issue22-runs.log`、`issue22-github.log`、`issue22-database.log`はそれぞれ共通Run、読取り専用観測、migration/停止/復元試験の成功。`issue22-check-final.log`、`issue22-unit-final.log`、`issue22-build.log`は静的検査、Go試験・UI 13ファイル41件、buildの成功。いずれも`test-results/`配下。 |

観測結果を固定するSHA-256は次のとおり。

| ファイル | SHA-256 |
| --- | --- |
| `test-results/issue22-independent-publications.log` | `1936015efc775eefd80d716aed8c90f6dd761e0b6ac098bffc15d072825b05f3` |
| `test-results/issue22-publication-ui.jpg` | `98861b58ffe12fbcff37ad9dade6da1d38242dcbb6a887aabc74ab211d8fd426` |
| `.cache/issue22-ui-run.log` | `26e0ca91a952c37d53ba1ba60063c30912bdc483a0b6b3205020baae6f95b321` |

## 条件ごとの結果

| 条件 | 確認した経路・操作と結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | 下書き保存の外部送信0、同じ固定下書きから人と内部自動化主体が共通`submit`へ入り、受付時は`accepted`・外部結果なし。UIは端末下書き、共有保存、受付、外部未確認を区別する。 | E1、E2: `service.go:147/240/245/265`、`postgres_test.go:156/181/194/306`、E3 | PASS | 内部自動化呼出しが対象。対象外のHopper条件評価を確認済みとはしていない。 |
| AC-2 | 固定接続ではrepository ID一件・Issues write/Metadata readでtokenを交換し、POSTの201だけでは確定せず対象IssueをGETして保存する。HTTP fixtureの取得内容は送信本文と一致する。 | E1、E2: `remote.go:109/126/141`、`postgres_test.go:40/211` | INCONCLUSIVE | 実GitHub Appによる許可済み検証repositoryへの作成・GET・共有記録の比較が未実施。固定接続の成功で代替しない。 |
| AC-3 | 同じ操作IDの受付再送は同じreceiptへ戻る。StoreはWorkspaceの行ロック後にreceipt/CASを確認し、要求とqueueを同じtransactionで保存する。並行4処理は`change`の今回適用有無で送信権を決め、外部POST一回へ収束した。 | E1、E2: `persistence/store.go:191/198/209/227`、`service.go:281/297`、`process.go:16/162/176`、`postgres_test.go:193/202`、E4のStore並行試験 | PASS | 起票専用試験の並行実行対象はWorker処理。並行受付は共通Storeのロック・receipt経路と既存並行試験を合わせて照合した。 |
| AC-4 | 作成後の応答喪失で`unknown`、未発見でも`unknown`を維持し、既存Issue照合で`created`へ進む。作成直後のプロセス消失も期限後に`unknown`へ移し、再作成せず照合する。422の確定拒否とは区別される。 | E1、E2: `process.go:125/135/197`、`remote.go:151/156/169/197`、`postgres_test.go:242/271/333` | PASS | 外部にない冪等性保証は主張していない。 |
| AC-5 | UIは編集内容と送信案を主体・Workspace別に保存し、復元時は取得・確認を案内するだけで送信しない。明示操作は現在の共有版と既存要求を先に確認する。提供された停止・復帰・再読込みの記録と画面はこの経路に一致し、サーバーでも保存・受付だけでは外部送信されない。 | E2: `publications.tsx:113/151/274/317/350`、`sync-store.ts:54/77`、E1の受付前後送信0、E3 | PASS | 提供観測とコードの照合による判定。GUI停止・復帰の独立再操作は担当範囲外として実施していない。 |
| AC-6 | 固定接続の手動編集を再取得して、元の共有下書きと別の外部本文として保存する。作成経路以外の外部更新送信はなく、422の理由と取得時刻を保存し再送しない。 | E1、E2: `process.go:35/197`、`remote.go:156/169`、`postgres_test.go:228/271` | INCONCLUSIVE | 実GitHub上で一回編集した本文を再取得し、共有下書き・更新送信0と比較する確認が未実施。確定失敗の固定試験部分はPASS。 |
| AC-7 | viewer・実行不許可・別Workspace・許可外接続の受付を拒否し、受付後の実行許可取消とHopper Control無効化では送信0。固定下書き版、現在の参照権限、現在の実行許可、Control版と有効状態、許可接続を送信境界で再確認する。 | E1、E2: `service.go:167/278/285`、`process.go:145/163/179/183`、`postgres_test.go:184/288/316/324/376` | PASS | 固定した取消例と送信境界を確認。取消前に発生した副作用の取消は要求していない。 |
| INVAR-1 | 要求から共通Runへ固定inputを作成し、`server-connector`として開始・原結果を記録する。端末割当はなく、ネイティブ割当による架空CLI化を拒否する。Workerはこの同じRunを使う。 | E1、E2: `process.go:157/179/190`、`runs/connector.go:16/22`、`service.go:303`、`postgres_test.go:219`、E4 | PASS | 新しい実行方式を追加していない。 |
| INVAR-2 | 共有下書き、送信本文、外部観測が別記録。refreshはGitHubから取得したタイトル・本文・状態・更新時刻を外部観測として保存し、共有下書きで上書きするAPIはない。 | E1、E2: `model.go:33/48/58`、`process.go:35/216`、`postgres_test.go:228` | PASS | 固定接続での正本の扱いを確認。実GitHubでの編集観測はAC-6に未確認として残す。 |
| INVAR-3 | `unknown`経路はObserveだけを呼び、新規Createへ戻らない。refreshは同じ要求とRun IDを保持し、終了済みRunの原結果を更新しない。プロセス消失後も同じRunを終了して外部照合を続ける。 | E1、E2: `process.go:70/108/125/135/197`、`postgres_test.go:245/333`、E3 | PASS | 外部の作成確認と新Run割当を混同する経路は確認されなかった。 |

## 共通処理と件数の照合

GitHub観測から抽出したtoken交換は、観測側が従来どおりMetadata/Issues/Pull requests/Contents/Actionsの五つのread権限を要求する。返却されたwrite権限を拒否する検査も残る。起票側は別設定でIssues write/Metadata readを要求する。秘密鍵をAPI応答やRunへ渡す経路はなく、共用範囲は鍵読取りとtoken交換である。E4の観測試験も成功していた。

共通Runの変更はConnectorの固定入力・開始・結果を既存記録へ保存する経路と参照元の追加であり、通常Runは既存の認可と割当経路を使う。E4のRun試験で、二Pause、再接続、同時回答、原出力、別Runの由来、現在権限の検証が成功していた。今回の差分から合意済みのRun/観測動作の回帰は確認されなかった。

一覧は最大100件のメタデータ、履歴は選択したownerと固定throughのページ、Workerは期限順25件で取得する。migration 018には一覧・履歴・期限queueの索引がある。通常の起票操作から全Context・全原ログを読む経路はない。E1では小さいページと履歴cursorも確認した。

## 指摘と判定

- 総合判定: INCONCLUSIVE。
- blocker: 固定条件の未達または今回による合意済み回帰として、到達経路と具体的な影響を確認できたものはない。
- 残る条件: AC-2の実GitHub Appによる作成・GET・共有記録比較、AC-6の実GitHubでの外部編集観測。本人再認証が必要で、検品時点では新しいApp作成・書込み許可・鍵生成・installationが未実施との引継ぎを受けた。未実施部分をPASSにしない。
- この判定だけでは区分Cの必要な検証完了やReady、Issue Closeを認めない。必要なCI・マージ後確認も本検品の実施結果には含めていない。

独立再実行はE1だけを行い、共通画面・Run・観測・復元・全体検査は指定された既存証拠を照合した。新しいテストケース・fixture・実装は追加していない。作業後もコードは固定commitと一致し、本記録だけを追加した。日本語はyomiyasuの意味保持・条件保持に沿って推敲した。

実接続を補完した後は、そのログ、外部識別子・取得内容・時刻、対象commitとの差分を入力に、AC-2とAC-6および差分が影響する条件だけを再検品する。上記の有限条件はすべて判定したため、今回の検品作業はここで終了する。
