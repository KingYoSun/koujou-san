# Issue #24: 固定成果物の独立検品

## 対象と入力

- 対象HEAD: `86819e6ac5bbe9894127dab50c96de7d9cad8d0a`。branch: `codex/issue-24-run-usage`。
- 基準main: `e651f9d8a24574f1a24f23a0da11df7c4a96db40`。Issue #24、Scope revision 1、区分C。
- 検品者: 実装会話を継承しない独立担当。実施日: 2026-10-17。開始時の作業ツリーに変更はなかった。
- 固定条件: [計画](2026-10-17-issue-24-plan.md)のAC-1〜7・INVAR-1〜3。条件は追加・変更していない。
- 入力仕様: `AGENTS.md`、`AGENTS.local.md`、[文書案内](../README.md)、[開発手順](../runbooks/dev.md)、[Issue手順](../runbooks/issue-lifecycle.md)、[ADR 0024](../adr/0024-run-usage.md)、[Run利用量](../runbooks/run-usage.md)、[料金と集計](../runbooks/usage-pricing.md)。
- 観測入力: [証跡](2026-10-17-issue-24-evidence.md)、[公開抜粋](assets/issue24-runtime.json)、指定された`test-results/issue24-*`のログ・実接続JSON・画面画像。初期失敗と回復前の失敗も別々に確認した。
- 対象: 採用CLIの機械記録変換、暗号化した別queueと同一ID再送、現在認可、終了後の遅着、Run別ページ、共通UIの数量・費用・固定根拠表示。
- 対象外: 全CLI・全内部消費の完全取得、新しい製品判断、OS実機の再操作。秘密fixture、OAuth・端末ファイル、プロバイダ資格情報、他案件履歴は読んでいない。
- 方法: Windows上で固定差分、実装、既存試験、観測を照合した。コード探索はCodeGraphから始めた。実CLI・ブラウザ・OS実機の操作、新規試験、source/testの変更は行っていない。

## 条件ごとの結果

表中のログ名は`test-results/`配下。PASSは固定した取得範囲と利用場面に限る。

| 条件 | 確認した経路と期待結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | stdoutの機械イベントを`Meter.observe`へ渡し、Run・割当世代・process・session・CLI版を保存する。Codexのモデルは明示設定した実行内OTelの同じsessionから取得する。実接続のモデル・数量・固定料金版は公開抜粋と一致した | `runner/usage.rs`、`model_capture.rs`、`runtime.rs`、`issue24-runtime-live.log/.json`、公開抜粋 | PASS | 実CLIは再実行していない |
| AC-2 | Claudeのモデル別終値で途中値を更新し、message IDの重複を除く。子処理・再試行を含む取得範囲と、範囲外の未取得を明示する。Codexの子sessionは未取得 | `usage_tests.rs`の複数モデル・重複message試験、`issue24-native-usage.log`、共通UI画像 | PASS | 採用範囲外の補助呼出しは条件どおり未取得 |
| AC-3 | 累積系列は同じUsage IDを更新し、既存coverage処理で重複を除く。再利用・forkの境界不明は`coverage=unknown`とし、通常のRun数量・CLI参考推定額へ採用しない | `usage.rs`、既存`usage/service.go`、`issue24-usage.log`、再利用・継続flagのネイティブ試験、`run-usage.test.tsx` | PASS | 後続計測の同期不具合はAC-6のB1として扱う |
| AC-4 | 終了・失敗・取消・Unknown後も計測だけを保存し、Run状態・原出力・commandを更新しない。送信と回復経路からCLIの再起動へ進まない | `runs/usage.go`、`runtime.rs`、`issue24-run-usage-server.log`、実接続のRun版一致、終了済みCodexの遅着送信記録 | PASS | OS操作は既存証拠の範囲 |
| AC-5 | 非対応版・plain・未確認版では計測を取得不可とし、計測の対応判定を実行可否へ追加しない。明示LLM非該当、一部取得、定額・ローカル・契約未確認を区別する | `Meter.supported/persist`、既存`verify_version`との分離、`issue24-native-usage.log`、`issue24-ui-unit-final.log` | PASS | なし |
| AC-6 | queue本体は暗号化し、同じevent・operation・送信内容を保持する。報告APIは現在端末・Run・世代・報告権限・依頼者・固定入力を保存transaction内でも確認する。ただし同一系列の途中値が受付応答を失うと、後続終値が古い期待版を固定して同期できなくなる | `journal.rs`、`usage.rs:390–433`、`runs/usage.go:43–90`、`checkAssignment`、既存ACK喪失・別Run巡回試験、B1 | FAIL | B1の修正と回帰確認が必要 |
| AC-7 | Run詳細から50件ずつ取得し、途中/終了・暫定/一部/取得不可・未算定・再評価待ち、モデル・内数・参考推定額を区別する。料金根拠のボタンは固定版を取得する | `run-usage.tsx`、`run-usage.test.tsx`、`issue24-ui-unit-final.log`、`issue24-codex-usage.jpg`、`issue24-claude-usage.jpg`、`issue24-price-evidence.jpg`、`issue24-ui.log` | PASS | 実操作の再実施はしていない |
| INVAR-1 | 計測の観測・終了保存・送信失敗を実行結果の成功/失敗へ変換しない。実行報告と計測queueは独立する | `runtime.rs:169–173,381–397,496–498`、`usage.rs`、Run版不変のPG・実接続証拠 | PASS | B1は計測の同期に影響し、CLI結果は変えない |
| INVAR-2 | 採用されたtypeと構造化usageだけを処理する。LLM本文中の偽数量は計測にならない | `Meter.observe`、`models_provisional_final_and_duplicate_messages_keep_machine_boundaries`、`issue24-native-usage.log` | PASS | なし |
| INVAR-3 | 計測へ保存する値は数量・モデル・相関・取得範囲・原記録参照とdigest。OTelではsession/model以外の属性を保持しない。既存認証・非保存引数を維持する | `usage.rs`、`model_capture.rs`、`execution.rs`差分、秘密文字列の除外試験、実接続JSON・公開抜粋 | PASS | 資格情報ファイル自体は検品入力に含めていない |

Run別取得は`usage/query.go`の`mode=run`で対象Runのキーを先に選び、各キーの最新値と現在権限を確認する。既存016の`usage_entry_run`索引に対応し、Workspace全件の最新値を先に作らない。認可には全依存を使い、返却する根拠参照は計測・料金・評価に絞っている。基準mainから既存SQL migrationへの差分はない。

## 指摘

### B1: 受付応答を失った途中計測の後続終値が同期できない

- 分類: blocker、優先度P1。直接関係する固定条件はAC-6。
- 入口: 採用版Claude Codeの「途中更新と終了時」を使う通常のRun。`Meter.persist`は同一モデルの途中計測Aと終了計測Bを、同じ`resource_id`でqueueへ保存する。
- 到達経路: `runner/usage.rs:390`は巡回位置より後の未送信行を選び、同じresourceの先行未受付行があっても後続を送る。`405–419`は初回送信時の`expected_version`を暗号化payloadへ固定し、以後更新しない。受付版を更新するのは応答を読めた`421–433`だけである。
- 具体例: Aを`expected_version=0`で送信し、サーバーが版v1として受理した後に応答だけを失う。次の巡回でBを選ぶと、端末の受付版がまだないため、Bも`expected_version=0`を固定して送信し、CASで拒否される。巡回がAへ戻ると同じoperationのreceiptからv1を回収できる。しかしBには既に期待版0があるため、以後も0のまま再送され、版v1との競合が続く。再起動でも固定済みpayloadは変わらない。
- 影響: 終了値Bを端末で保持していても共有へ反映できず、Runの利用量が途中の暫定値のまま残る。出力数量・CLI参考推定額・終了時の取得状態も届かない。CLIの結果と原出力は変わらない。
- サーバー側の根拠: `persistence/store.go:210–232`は同じoperationのreceiptを返し、別operationでは現在版と期待版を比較する。後続Bの期待版を自動的に差し替える経路はない。
- 既存試験の範囲: `unsent_encrypted_usage_survives_reopen_and_lost_ack_with_same_operation`はqueue一行、`one_denied_report_stays_pending_without_blocking_another_authorized_run`は異なるRun二件を確認している。同一resourceのA受付応答喪失とBの累積更新を組み合わせた経路は覆っていない。
- 判定方法: 固定HEADのコード経路とCAS・receipt契約の照合。新規試験や実CLIによる再現は行っていない。
- 再検品する差分: 同一resourceの受付順を守りながら、拒否された別Runの計測を保留して他Runへ進めること。A受理・応答喪失、Aの同一operation再送でreceipt回収、Bの正しい期待版での受付までを確認する。AC-6と、影響するAC-3・INVAR-1を再確認する。

## 観測と限界

- 既存の静的検査・全体試験: `issue24-check.log`、`issue24-test.log`を確認した。Goの各packageと共通UI 15ファイル50試験の成功が記録されている。静的検査のログには失敗の出力がない。静的検査の終了状態は提出された証跡による。
- 局所試験: `issue24-ui-unit-final.log`は2ファイル9試験成功、`issue24-native-usage.log`は14成功・4 ignored、`issue24-check-desktop.log`はコンパイル成功。ignoredの実接続を局所試験の成功へ数えていない。
- 専用PG: `issue24-run-usage-server.log`のRun別計測・現認可・遅着・再送・結果不変が成功。`issue24-usage.log`では既存の十進数、累積・遅着・親子重複排除、1万件×3版が成功している。
- 実接続: `issue24-runtime-live.log`はignored testを明示実行して1件成功。実接続JSONと公開抜粋のRun ID・モデル・終了状態・固定料金版・推定額が一致した。Codexの0.025039 USD、Claudeの0.011187 USDは検証用個別単価による推定額で、実請求額として扱っていない。
- 共通画面: 三枚の画像でモデル・数量・取得範囲・CLI参考推定額・単価評価・固定料金版の表示を確認した。`issue24-ui.log`は専用suite成功とサービス・volumeの削除を記録している。keyboard操作の実施は提出された観測記録による。検品中に本人操作は求めていない。
- 初期失敗: `issue24-runtime-initial-stall.log`は成功終了を記録していない。起動通知のdecoder拒否は証跡の説明と照合し、現HEADの起動通知が従来形式であることを確認した。`issue24-runtime-recovery-before.log`の`forbidden`失敗も成功へ数えていない。終了済みCodexの遅着送信と、その後のClaude新規Runの成功だけを実接続の成功証拠に用いた。
- 実接続・画面確認後の返却参照絞込み、未取得/非該当の文言、送信巡回は、固定差分とPG・UI・ネイティブの局所証拠で確認した。最終HEADの実CLI・共通画面を再実行したとは扱っていない。
- この検品で既存suiteを追加実行していない。B1は既存試験の再実行だけでは判定を変えられない。

## 総合判定

- 総合結果: FAIL。blockerはB1の一件。AC-6が未達のため、このHEADを受入完了とは判定しない。
- その他の固定条件について、今回の範囲内でblockerとなる未達・回帰は確認しなかった。
- 未確認: B1修正後の差分検品、最終HEADの必須CI、merge後の内容照合。最終CIやmerge後照合が未実施であることを、局所試験の成功で置き換えていない。
- 検品記録はyomiyasuで推敲し、同梱lintは終了0。箇条書き比率の候補一件は、固定入力・観測・判定を並べる検品記録の用途に合うため維持した。新規文書への`git diff --no-index --check`も確認した。

## B1修正後の差分検品

2026-10-17、対象を`155c02cf547b72bf68abfe15d094a14897b9bbc1`へ固定した。前回HEAD `86819e6ac5bbe9894127dab50c96de7d9cad8d0a`からの差分、証跡末尾、`test-results/issue24-cumulative-ack-before.log`と`issue24-cumulative-ack-after.log`を入力に、B1・AC-6と影響するAC-3・INVAR-1だけを再検品した。前回の条件と禁止範囲は維持し、実CLI・ブラウザ・OS操作や追加試験は行っていない。

`runner/usage.rs:392`は、同じ`resource_id`に先行する未受付行がないことを送信条件に加えている。途中計測Aの受付応答を失っても、同じ系列の後続計測は選ばれない。巡回がAへ戻り、同じpayload・operationで受付版を回収した後に、後続計測の初回送信がその版を使う。別resourceはこの条件に遮られず巡回できる。`journal.rs`の追加索引は未受付行のresource照合を支え、暗号化payload・実行queue・サーバーの契約は変えていない。

| 再検品対象 | 確認した結果と証拠 | 判定 |
| --- | --- | --- |
| B1 / AC-6 | 変更前ログは、Aの同内容再送を期待した箇所で別operation・後続sequenceの計測が選ばれてFAIL。追加試験はAの応答喪失、Aの同内容再送とv1回収、次の途中値をv1で受付、終了値をv2で受付、未送信0件まで確認する。変更後ログで同試験が成功し、既存の別Run巡回試験も成功 | PASS、B1解消 |
| AC-3 | 取得・累積系列・Usage ID・coverageの生成は差分に含まれず、変更は系列内の受付順だけ。追加試験でも、同じAの再送は同内容・同operation、後続計測は別operationと受付済み期待版を使う。既存の重複message・再利用・継続flag試験も成功 | PASS維持 |
| INVAR-1 | 修正は計測queueの行選択と索引に限られ、Run報告・CLI起動・終了処理への差分はない。追加試験は最後まで`execution_frames`が0件であることを確認する。変更後の既存Runner試験も成功 | PASS維持 |

`issue24-cumulative-ack-after.log`はRunner試験15成功・失敗0・4 ignoredを記録している。4件の実接続・所有環境試験を実行済みとは扱っていない。追加試験の受付応答は固定応答であり、前回検品した既存サーバーのCAS・receipt契約と合わせて判定した。サーバー・共通UIへの変更はなく、前回の実接続・画面証拠を再利用した。

このHEADの独立検品の総合結果はPASS。B1は解消し、blockerは0件。AC-6をPASSへ更新し、他のAC・INVARは前回のPASSを維持する。前節のFAILは前回HEADの履歴として残す。最終HEADの必須CIとmerge後の内容照合は引き続き未確認であり、Issue全体の完了をこの検品だけで確定しない。

追補もyomiyasuで推敲した。lint・`git diff --check`はいずれも終了0。lintの候補二件は、検品項目の箇条書きと既存契約に合わせた「版」の表記であるため維持した。
