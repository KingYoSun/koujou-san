# Issue #24: Run利用量の証跡

- 基準main: `e651f9d8a24574f1a24f23a0da11df7c4a96db40`。Scope revision 1、区分C。[固定計画](2026-10-17-issue-24-plan.md)のAC-1〜7・INVAR-1〜3を維持する。
- 現在の判定は[current-state](current-state.md)を参照する。独立検品・最終head CI・マージ後照合は未完了。

## 実装と保存境界

既存UsageRecordへ機械取得のmetadataを追加し、同じCAS・receipt・coverage・料金評価を使う。Run報告は従来の形式を維持し、計測を独立した暗号化queueから送る。現在の端末・割当世代・実行依頼者・固定入力をtransaction内でも確認し、Runと入力の権限を計測の参照・集計・料金へ引き継ぐ。終了後の計測でRun状態・原出力・commandを更新しない。

Run別ページは既存の016の索引から対象Runのキーを選び、その最新値と現在権限を照合する。Workspace全件の最新値を先に作らない。権限の確認には依存全体を使い、画面へ返す参照は計測・料金・評価だけにする。Context依存を画面へ余分に送らない。新規migrationはなく、001〜019にも変更はない。

Codexの端末内OTel取得は接続ごとの明示設定で、既存設定を自動変更しない。sessionとmodel以外の属性を保持しない。会話履歴の保存と元CLIの認証を変更せず、原出力を計測記録へ複製しない。Claudeの途中出力placeholderとLLM本文中の自己申告を数量にしない。再利用・forkの累計は確認対象とし、今回の確定数量や費用へ採用しない。

## 局所試験

- `test-results/issue24-run-usage-before.log`: 実装前は計測入口がなく、利用量POSTが401で失敗した。
- `issue24-run-usage-server.log`: 専用PG成功。終了・失敗・取消・Unknown後の計測、同一操作再送、異なる端末・世代の拒否、複数モデルのページ、現在の非公開Runの非表示、Run結果の不変を確認した（最新の結果は同ログ）。
- `issue24-usage.log`: 既存PGの十進数・原記録・累積と遅着・親子・取得元の重複排除、1万件×3版が成功（package 7.076秒）。
- `issue24-native-usage.log`: 収集・暗号化保留・再開・応答喪失時の同一operation、拒否を保留した別Runの送信、モデル/子処理の範囲、CLIごとの再利用flag、取得不可/非該当/一部取得、元実行記録を変えないことを確認。端末内モデル受信でもsession/model以外を保持しない。
- `issue24-check.log`、`issue24-test.log`: 静的検査・書式・Goと共通UI試験成功。`issue24-check-desktop.log`はネイティブのコンパイル確認成功。`issue24-client-build.log`は共通UI build成功。

## 実CLIから共有API・固定料金への接続

Windows Codex `0.162.1`とClaude Code `2.1.300`を、専用Ory/API/PGのWorkspace `runner-ui`へ登録した端末で実行した。既存認証を使い、CLIは短い検証応答だけを返す。Windowsアプリの起動・ログイン・スリープは再確認していない。

`runner::runtime::usage_tests::live_owned_runner_usage`で実際の`run_one`を使い、固定入力の取得、実プロセス、暗号化journal、現在の端末認可による計測報告、Run別取得、固定した検証用個別単価の評価を照合した。最終試験は成功（8.29秒）。ログは`test-results/issue24-runtime-live.log`、数量・相関・料金版・評価の記録は`issue24-runtime-live.json`。公開用の[数量・相関・評価の抜粋](assets/issue24-runtime.json)も保存した。JSONへ資格情報・全文対話を含めていない。

| Run | 機械取得モデルと数量 | 固定した検証単価の評価 | 実行結果 |
| --- | --- | --- | --- |
| `run:usage-live-0-24100f34-0962-4dc1-b092-5c9af85d7aa4` | `gpt-6.1-sol`。入力25034（cache read13440、cache write0を含む）、出力5、推論0 | 0.025039 USD、estimated/rated | succeeded、exit 0。計測同期前後のRun版が一致 |
| `run:usage-live-1-aad57b10-6e38-4b18-a1bb-c0a473d1719b` | `claude-opus-5-5`。入力2、cache read6890、cache write4291、出力4、推論0 | 0.011187 USD、estimated/rated | succeeded、exit 0。計測同期前後のRun版が一致 |

単価は各非重複tokenに1 USD/100万tokenを適用した検証データで、情報源は`fixture://individual-contract`。実契約の請求額ではない。Claude CLIの参考推定額0.035794 USDとも別の値として表示した。

初期の一巡試験では、起動通知へ追加した`execution.usage`を既存の厳密なdecoderが拒否した。CLIは終了したが開始以降の報告が未受付となった（`issue24-runtime-initial-stall.log`、`issue24-runtime-initial-stall.json`）。起動通知を従来の形式へ戻し、計測側へ分離した。旧試行`run:usage-live-0`のUnknownを成功証跡に含めない。

次の試行はCLI終了と共有結果の保存まで成功したが、旧試行の受理不可計測が送信先頭に残り、接続試験がforbiddenで失敗した（`issue24-runtime-recovery-before.log`）。専用fixtureの旧試行一件の計測だけを破棄して試験を継続した。製品では拒否を保留しつつ別Runを送信できる巡回を追加し、単体試験で確認した。破棄を製品の既定動作にはしていない。

上表のCodexは終了済みプロセスを再実行せず、保持した同じ計測を遅着として送った。最後の一巡試験はそのRunを再利用し、Claudeの新規Runと合わせてAPI受付と料金を確認した。修正前の失敗や未確認を成功へ読み替えていない。

## 共通画面

実Ory、1万Context×3版の同じ所有環境で、共通UIから実CLIのモデル・数量・終了時取得・契約未確認・CLI参考推定額・単価による推定額を確認した。キーボードで作業を選び、料金の根拠を展開し、固定料金版を取得した。料金版APIへ送れないRun参照は料金リンクへ出さない。報告額がない表示は未取得・未算定で、0費用にしなかった。

画像は`test-results/issue24-codex-usage.jpg`、`issue24-claude-usage.jpg`、`issue24-price-evidence.jpg`。状態別UI試験は暫定・一部取得・取得不可・LLM非該当・未算定・遅着後の再評価待ちと、境界不明の数量・金額を通常表示へ採用しないことを確認した。

画面の確認後に所有fixtureの`finish`を送った。UI suiteは成功（試験1044.58秒、package 1046.619秒）。結果は`test-results/issue24-ui.log`。専用サービス・volumeは終了処理で削除し、秘密fixtureとOAuth・端末ファイルも削除した。共有CAと標準開発サービスは削除していない。

画面と実接続の取得後、Run別APIの返却参照を計測・料金・評価へ絞った。権限の確認に使う依存は削らず、対応する専用PGと表示側の契約を照合した。最終の未取得/非該当の表現と送信巡回の差分は局所UI・ネイティブ試験で確認し、実CLIやOS操作を繰り返していない。

## 独立検品で見つかった送信順の修正

固定HEAD `86819e6`の検品では、途中計測の受付応答を失うと、同じ系列の後続計測が古い期待版を固定して送れなくなるB1を確認した。回帰試験の初回はfixtureの保存件数の想定を直し、その後の変更前試験で、先行計測の再送より先に後続が選ばれることを再現した（`test-results/issue24-cumulative-ack-before.log`）。

同じresourceでは最古の未受付行だけを送信対象にし、別resourceは引き続き巡回するよう修正した。resource別の未受付索引も追加した。応答を失った計測を同じ内容・operationで再送して受付版を回収し、後続の途中値・終了値を正しい期待版で共有へ届けることを確認した。拒否された別Runは保留し、許可されたRunは送信できる。元の実行queueには書き込まない。

変更後の`runner::`試験は15成功・4 ignored、失敗0（`test-results/issue24-cumulative-ack-after.log`）。実CLI・ブラウザ・OSの確認は再実施していない。差分の独立検品と最終CI・merge後照合は、この時点では未完了。

## 完了時の照合

`155c02c`の差分検品は全条件PASS、blocker 0。その後は検品追補と進捗記録だけを変更した。最終head `daca90b4754c5b0017b22e6ba7ff1dbb3767b1bf`の[CI 39026136641](https://github.com/KingYoSun/koujou-san/actions/runs/39026136641)で共通UI・サーバー、Windowsネイティブ、GitGuardianが成功した。

[PR #58](https://github.com/KingYoSun/koujou-san/pull/58)を2026-10-17 10:53:35 UTCにマージした。main `a7c8b56151f51e876844ae2810f59d561b81a631`と最終headのtreeは`ab5d04c8ebe94a50b2394aa9731b3e7055fb9acf`で一致し、差分0。#24を手動Closeし、親#2の一覧も完了へ更新した。元の試行失敗・取得範囲・未取得内訳は上記のまま保持する。
