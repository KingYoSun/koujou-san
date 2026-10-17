# Issue #18: CLI Runnerの検証

受入範囲は[計画](2026-10-17-issue-18-plan.md)のScope revision 1。初回の実接続に加え、09:02にWindows画面終了後の継続とnative設定の実操作を確認した。最新の判定は[現在状態](current-state.md)を参照する。

## 実装と実接続

Windows用の独立Runner binaryを追加した。端末資格情報で担当Runをページ取得し、現在の割当・入力・端末内programを照合してchildを作る。固定入力、原ログ、実行対応、未送信報告をローカルへ記録し、GUIのOAuthセッションと実行状態を分ける。Runごとのworktree・branchは#17の準備処理を再利用し、WSLのGitも接続した。

専用Ory/API/PG fixture `8c48a6da2b5c`（API `https://127.0.0.1:56326/`、Workspace `runner-ui`）で、SDK不要のPythonスクリプト、Windows Codex `0.162.1`、Claude Code `2.1.300`、Ubuntu-22.04のPythonを実際に起動した。[機械可読の結果](assets/issue18-runtime.json)に固定Run版・実環境・出力件数・exportのdigestを記録する。標準開発サービスは変更していない。

| 条件 | 確認済みの結果 |
| --- | --- |
| AC-1・INVAR-2 | 通常スクリプトのstdin・stdout/stderrと終了0を対応付けた。原文のstderrも読戻した |
| AC-2a | Windows/WSLでファイルのContext本文と引数のpromptが一致。prompt中の`{context}`はそのまま保持。構造化stdinも元値と一致 |
| AC-2b | `cli-unsupported`は`input_binding_unsupported`で開始前に停止し、入力を保持 |
| AC-3 | 未登録targetは割当なし。API試験で旧世代・現在の実行権限/端末許可の失効を拒否。prepare・取得・previewからchildを作らない経路も照合する |
| AC-4 | CodexとClaudeへ同じContext・prompt・結果schemaを渡し、終了0・派生判定PASS。接続先以外の入力と判定条件を維持 |
| AC-5 | 二つのPauseの受領・配送・再開を別々に保存し、両方resolved。自然文の疑問文は原文のままでPause 0件 |
| AC-6 | 停止対応childは実終了後Cancelled。非対応childは取消要求後も続き、12秒後の自然終了をSucceededとして記録。双方の既存ファイルは保持 |
| AC-7 | Web版の参照を確認。CLI未設定のWebから二回答を明示送信し、両方の配送・再開と終了0を確認した |
| AC-8 | 最終の8 Runの共有記録・exportに合成秘密値、ホーム、実path、CLI内部の初期化情報がないことを照合。端末参照のログは未取得と記録 |
| INVAR-1・3 | 実version probeと環境・停止/対話能力を記録。CLI自身の権限を制限できるとは表示しない |

WindowsとWSLのrepository付きRunも実行した。固定commitから異なる専用branch/worktreeを作り、それぞれの作業領域に合成成果を保存した。元のWindows checkoutはHEAD・作業状態とも変わらない。

## 検出と修正

WSLの初回実行では、Windowsのpackaged-app保存先のjunctionをDrvFsが解決せず、作業場所への移動に失敗した。WSLへ渡すpathを実体pathへ変換し、同条件の入力往復とWSL Gitの実行を確認した。Windows Git向けの短いpathは維持した。

Codexの`item.completed`には、ホーム内の設定pathを含む`error`イベントもあった。共有出力への混入を再現するテストを先に失敗させ、診断ログの未取得参照へ切り替えた。`cli-codex-fixed`の再試験で原回答・終了0・PASSを保ち、共有記録とexportから当該pathが消えたことを確認した。修正前の合成Runは成功証跡へ混ぜていない。

固定ManifestはSpec.input.contextを持たないため、最初の入力実装では本文が落ちた。実際のSpec形状による失敗テストを残し、Manifestの`input_utf8`を直接渡すよう修正した。本文の空白・改行と、任意JSONの大きな数値も照合した。[実Runnerの結果](assets/issue18-manifest.json)でも、CLIが受けた本文と固定Manifestの完全一致・終了0を確認した。

## ローカル検証

root check/test/build、desktop check/test、専用PGの`test:runs`を実施した。root UIは11 files / 36 tests、desktopは9 passed / 6 ignored。ignoredのうち専用OAuth/device準備は所有fixtureへ明示して実行した。`test:runs`は2.95秒でPASS。終了報告のHTTP応答が失われた場合、完了後・lease切れ後も同じreceiptを返し、新しい報告で完了状態を書き換えないことも確認した。

desktop buildは、稼働中の同じRunner binaryを上書きしようとしてWindowsのfile lockで一度失敗した。所有Runnerの受付を止め、終了後の再実行でdesktopとRunnerの両binaryをbuildできた。共通UIの回答修正後の再buildも成功した。リンク時の既存PDB名の警告は別に記録し、build成功と区別する。

## 初回の画面検証（04:40時点）

Chromeで専用Workspaceと停止非対応Runを表示し、実環境・版・能力、取消要求と自然終了が区別されることを確認した。終了後も「実終了を待つ」と出る文言を、実行中だけの表示へ修正した。

CLI未設定のWebで二回答を送信した[画面](assets/issue18-web-dialogue.png)と[記録](assets/issue18-web-dialogue.json)を保存した。初回はRunnerのheartbeatでRun版が進み、表示時の版による回答が競合した。回答ボタンで現在のRunと同じPauseを確認してから送るよう修正し、heartbeat後の明示送信・配送・再開を確認した。別の質問へ回答を流用しない。

Windows Computer Useのwindow一覧は取得できている。一方、検証用Windowsアプリの起動が自動承認レビューに拒否された。実行ファイル・所有fixture・接続先を読み取りで照合し、起動だけへ限定しても`blocked by policy`となった。理由の詳細は返されていない。ユーザーへ起動のみを依頼し、残りの実操作はこちらで進める。まだ画面終了後の実Runner継続を達成扱いにしない。

## 独立検品による修正

一次の固定commit `d2d41f8395cc387bca73723c3118a3ec3cf1e5e5` を、会話履歴を継承しない担当が検品した。無注入Manifestの`input_utf8`省略を未対応扱いにするAC-2aの未達が一件あった。製品関数を使う失敗テストと、担当側の別crateでの再現が一致した。

`state:none`ではcontextを省略するよう修正し、局所テストと[実Runner](assets/issue18-none.json)でpromptだけがCLIへ渡り、終了0となることを確認した。別クライアント条件も追加で確認した。専用fixtureの別利用者BobはCLI・端末登録なし、編集者・実行依頼不許可のまま二Pauseへ回答し、[配送/再開と終了](assets/issue18-bob.json)を確認した。[画面証跡](assets/issue18-bob.png)も保存した。修正固定commit `636dcbb9b675a819d358d98c2e688c6e31aa26f2` の[再検品](2026-10-17-issue-18-inspection.md)でコードblockerが解消し、新たなblockerがないことを確認した。

PRは[#51](https://github.com/KingYoSun/koujou-san/pull/51)。04:40時点では画面終了後の継続・新しいnative設定の実操作が未確認で、draftを維持した。

専用Ory/MCP試験は `PASS 1783.17s` / package `1785.227s` で正常終了した。所有Runnerのactive 0・未送信なしを確認して受付を止め、実終了後に専用環境のサービス・volumeを終了した。今回作成したOAuth/deviceのDPAPIファイルだけを削除し、標準サービスと他の資格情報は変更していない。GUI起動の依頼は一時環境終了により取り下げ、再開時に環境を用意し直す。

## Windows画面の受入（08:48〜09:03 JST）

ユーザーが起動したWindowsアプリをそのまま使った。専用fixture `f6abe538a444` を同じAPI originで作り直し、証明書のSHA-256が変わらないことを照合した。新しい認証主体と端末を使い、前回のDBや権限を再利用していない。

Codex配下のプロセスと通常のWindowsプロセスでは、同じAppData表記が異なる実体を指していた。WSLから双方の実体を確認し、未使用と確認した今回のnamespaceへ、合成fixtureのDPAPIファイルと宣言済み接続だけを用意した。実行接続の`execution.json`は事前に作っていない。

Windows画面でprogram・引数・明示JSONL対話・版確認の引数を入力し、保存ボタンを押した。[保存完了の画面](assets/issue18-native-settings.png)と実ファイルを照合した。保存時点では実行journalがなく、CLIを起動していない。

画面の「独立Runnerを起動」と「状態を更新」を使い、PID 84816がGUIのPID 59204から起動したことを確認した。合成Run `native-background-dialogue`を割り当て、実CLI PID 97764の起動と版確認、最初のPauseをAPIと[Windows画面](assets/issue18-native-before-close.png)で確認した。

09:00:19.279に通常の終了操作でGUIを閉じた。GUIプロセスとウィンドウが消え、同じRunnerとCLIのPIDが残ることを照合した。09:00:54.952と09:01:16.736にWeb画面から二回答を送信した。最初の回答後もCLIのPIDは同じで、両Pauseは配送・再開確認を経てresolvedとなった。09:01:17.451に終了0・Succeededを記録し、CLIプロセスは終了した。Runnerはその後も受付中・active 0・未送信なしで継続していた。

[プロセスの観測](assets/issue18-native-processes.json)、[固定Run・二Pause・原出力](assets/issue18-native-background.json)、[画面終了後の正常終了](assets/issue18-native-after-close.png)を保存した。これにより、初回に未確認としていた二つのWindows操作を確認した。実OSのスリープ・再起動を行った結果へは広げない。

最後に所有Runnerの受付を止め、実終了を確認した。専用Ory/MCP試験は `PASS 745.48s` / package `747.575s` で正常終了し、そのサービス・volumeを終了した。双方の保存先に作った今回のOAuth/device DPAPIファイル4件だけを削除した。表示に残っていた「ネイティブの実行機構は未実装」という旧文言も、合意済みの画面終了後の継続を説明する文言へ訂正した。
