# Runの入力・質問・出力・実行記録

Workspaceの「Run・入力・Pause・出力」でRun一覧を取得するか、Run IDを指定する。各取得は一ページずつ行い、「次のページ」で続ける。Context全件や全Run履歴を一括表示しない。

## 作成と開始依頼

編集できるメンバーは「新しいRunを作成」で接続の種類、command・接続先、引数/要求を指定する。contextとpromptは任意。既存の固定Manifestを使う場合はIDと版、入力の受け渡し方法を指定する。ManifestのRun IDへ結び付ける。作成ボタンからCLIは起動しない。

実行依頼の許可を持つ利用者は入力確定・開始依頼を保存し、担当端末へ割り当てられる。独立Runnerは現在の割当と端末内の接続設定を照合してCLIを起動する。依頼・準備完了・プロセスの起動確認は別々に表示する。

開始後の入力・command・目的は書き換えない。「現在のRunを新しいRunの関連元にする」を使って別Runを作る。元Runの会話や全履歴は自動で取り込まない。必要なContextとpromptは新しい入力へ明示する。

## Pauseへの回答

「質問と回答」を取得し、質問・条件を確認して回答を送る。そのRunを編集できれば、実行依頼の許可がなくても回答できる。構造化値が必要なときはJSONとして指定する。通信が戻っても回答を自動送信しない。

受領済み、配送確認済み、再開確認済みは別状態。受領の表示だけでcommandが進んだとは扱わない。競合したときはRunを更新して、既に受理した回答を確認する。解決した質問と回答は履歴に残る。外部イベント待ちは担当接続の観測で応答を記録し、人の回答と由来を分ける。

## 状態と出力

接続断は観測不明として最後の実行状態を残す。取消要求だけでCancelledにせず、実行側の確認を待つ。終了コード0と検品PASSは同じではない。派生契約の形式不正は利用可能な判定にしない。

原出力、報告履歴、確定結果、後続Runとの関連を切り替えて取得する。値、チャンネル、順序、出力元を保存し、GUIの要約で原出力を書き換えない。固定したinputを取得すると、command、任意のcontext/prompt、Manifest版、派生契約と由来を確認できる。

## 実行記録の書出し

選択中の固定版をNDJSONへ書き出す。入力、Manifest固定内容、Pause/回答/配送/再開、部分出力、派生判定、前後Runの関連、未観測・未取得を保持する。権限外の記録と端末資格情報は返さない。

Webはブラウザからダウンロードする。WindowsはDownloadsへ保存し、保存先を画面へ表示する。completeフレームを確認できないファイルはpartialとして残し、完了扱いにしない。接続・現在権限を確認してから同じ固定版を再度書き出せる。exportは元CLIやGitHubへ変更を送らない。

## 検証

`pnpm run test:runs`は専用PGで共通契約・競合・現在認可・ファイル出力を確認する。実Ory/MCPは`pnpm run test:mcp`、共通GUIの実操作は`pnpm run test:mcp:ui`を使う。ネイティブはdesktopのcheck/test/buildとWindows実操作で確認する。検証用の割当・固定commandのイベントはfixtureであり、製品Runnerの完成とは扱わない。

## 担当端末と作業領域

Windowsでは「この端末のRunner設定」で、接続の論理名・確認した版/環境/能力・CLI認証主体への参照・依頼を許可するメンバーIDを登録する。端末登録とWorkspaceでの入力取得/報告許可も必要である。repositoryを使う接続には論理IDと端末内pathを設定する。秘密値を入力しない。接続設定の保存はcommandを起動しない。

実行依頼を許可された利用者は、起動前Runの「担当端末へ割り当てる」で端末ID、必要能力、任意のrepository論理IDと固定commitを指定する。不足があれば準備待ちとして表示する。競合時はRunを更新して現在の担当を確認する。端末登録やWorkspace参加だけで実行許可を得ることはない。

Windowsの「この端末で割当を確認・準備」は、現在の許可と世代を照合して接続を更新し、Run専用worktree・branchを自動作成する。元checkoutは動かさず、再試行で編集中のファイルを初期化しない。pathは端末内の完了表示だけへ返す。この準備操作からCLIは起動しない。独立Runnerも同じ作業領域を使う。

Runnerの接続確認・割当の有効期間は2分。接続が古い場合は保存済みのRunner接続を更新する。期限切れや権限取消はCLIの停止確認ではなく、同じRunを別端末へ自動配布しない。終了したRunのworktree・branchも自動削除しない。

## CLI接続と独立Runner

Windowsの「この端末のCLI実行と独立Runner」で、共有接続の論理名を端末内のprogramへ対応付ける。program・引数template・作業場所・環境変数への参照はこの端末だけに保存する。保存操作はRunを起動しない。既存の接続設定とは別のファイルへ保存し、既存worktreeや入力を移行・初期化しない。

通常のCLIは、共有commandのparametersを文字列の引数配列として渡す。context/promptは宣言したbindingを使う。stdinで一つの値を渡すか、JSON objectの設定で両方を渡せる。引数/ファイルでは`{context}`・`{prompt}`・`{context_file}`・`{prompt_file}`を使う。値中の同名の文字列は再展開しない。固定Manifestは確定済みの`input_utf8`を渡し、省略した入力を補完しない。未対応の入力方式は開始前に準備待ちとして記録する。

「独立Runnerを起動」から担当Runの受付を始める。「独立Runnerの状態を更新」で稼働・接続・実行中・未送信記録を確認する。画面を閉じてもRunnerと許可済みのCLIは継続する。「新しいRunの受付を停止」は実行中のCLIを止めず、記録の送信後にRunnerを終了する。端末起動時の自動起動は設定しない。

Runnerは担当Runの索引付きページを取得し、開始時に必要な固定入力だけを取得する。Context全件や過去出力を巡回しない。開始前に現在の権限・割当世代・利用方針を再確認する。GUIのOAuthではなく、Workspaceで許可された端末資格情報を使う。実行の対応記録・固定入力・原ログ・未送信報告は端末内のSQLiteへ保存し、本文はWindows DPAPIで暗号化する。入力方式がファイルの場合はCLI用ファイルも端末に作成する。

WSLではdistributionとWSL内programを指定する。WSL内repositoryのpathは論理IDごとに対応付けられる。Windowsのアプリ保存先は実体pathへ変換してWSLへ渡す。実pathを共有接続へコピーしない。

版を調べる引数を設定すると起動前に実行し、接続の版と一致することを確認する。未設定の場合、版表示は接続の申告値として区別する。環境変数は必要なOS変数と明示した参照元から構成する。秘密値そのものを設定・Run.inputへ記入しない。CLI自身が持つファイル・ネットワーク等の権限をkoujou-sanが自動で狭める保証はない。

## 出力protocolと能力

通常CLIのstdout/stderrは原文またはバイナリのbase64として記録する。Codex exec JSONL / Claude print JSONLの接続例は、同じinputと派生契約を使い、commandだけを交換できる。結果schemaの引数には`{result_schema}`または`{result_schema_file}`を使う。CLI初期化・設定警告・stderr等の診断情報は端末の原ログを保持し、共有側には未取得の原ログ参照を記録する。要約や判定で原出力を上書きしない。

「明示JSONL対話」は最初に`{"type":"input","input":{...},"parameters":...}`をstdinへ渡す。CLIが`{"koujou_event":{"type":"pause","pause":{"id":"first","reason":"質問","request":...}}}`をstdoutへ出した場合にPauseを記録する。回答は`{"type":"answer","pause_id":"first","response_id":"...","value":...}`として配送し、CLIの`koujou_event`による`resumed`と同じIDを照合する。`koujou_event`の`result`は`report`を結果として報告する。自然文の疑問文はPauseにしない。Codex/Claudeの非対話接続にPause能力を付けない。

停止対応を確認したWindows childだけ、取消要求で停止を試みる。実際の終了後に取消確認を報告する。WSLを含む未対応接続では実終了を待ち、取消要求だけでCancelledにしない。既に作った外部成果を取り消したとは扱わない。Runner再起動後に起動途中・実行中の記録を見つけても、自動で同じCLIを起動し直さない。完全な再接続・セッション復旧は#19の担当である。
