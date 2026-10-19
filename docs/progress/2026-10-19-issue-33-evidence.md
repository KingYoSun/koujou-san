# Issue #33: kukuri #1887の一巡受入記録

現在の条件と手順は[計画](2026-10-19-issue-33-plan.md)、現在の判定は[current-state](current-state.md)を参照する。実施中であり、初期リリース受入を完了したとは扱わない。

PR #70の実装差分は[独立コード検品](2026-10-20-issue-33-code-inspection.md)でR1〜R4がPASSとなった。初見操作・実S3・全体停止後の継続を含む#33全体の独立受入検品は、残る観測の取得後に行う。

## 条件付きマージとexport

2026-10-20（JST）、kukuriの固定候補 `82ff9f50d37b89ad8c7b58b9ba1baadb2a719b8d` に対するFast（39277853160）、Linux Package（39277852971）、Windows Store Package（39277852972）、Refactoring Audit（39277852977）が成功した。Fastの任意macOS・Androidジョブはskipであり、実行成功とは扱わない。

通常の工程 `workflow-execution:1887-final-merge` は、固定した独立検品結果、Fast、Linux Package、Windows Store Packageの四つのPASSを得てからmergeノードを開始した。検品ノードは既存の固定結果を読み取る照合であり、新しい独立検品を行ったとは扱わない。merge用のRunは `gh pr merge --match-head-commit` を実行し、[kukuri PR #1890](https://github.com/kukuri-app/kukuri/pull/1890)を16:25:04 UTCにマージした。工程の最終版は `FFRAUDYBD5HMQPZDKEC764QILM`、countは5、stateはcompleted。外部操作の再実行はしていない。

merge `92299fb938106e6a0c506128d999b176f7e336c3` と候補のtreeは、ともに `6e0821b96509932b19cbbc68cbdff9dde76e9441` で一致した。元のkukuri checkoutは変更せず、Runのworktreeでfetchと差分0を確認し、Issue #1887をCloseした。

対照例 `workflow-execution:1887-stale` は、対象を新候補へ変えて旧commitの検品PASSを参照させた。照合はINCONCLUSIVE、mergeノードはskipped、merge用Runは未作成となった。旧版のPASSを新しい成果物へ流用していない。

五つの固定版を通常APIからNDJSONでexportした。旧PASSの対照工程・完了したmerge工程・開始時刻を待つsleep工程と、最終検品Run・実装Runが対象である。全て末尾のcompleteとerrorなしを確認し、ファイルごとの固定版、件数、サイズ、SHA-256を `.cache/issue33-native/exports/manifest.json` に保存した。未取得の添付はその状態を維持し、全ての外部成果物をコピーできたとは扱わない。既知の試験用OAuth・DB・APIキー・認証用秘密値との完全一致検査では、いずれのexportにも一致しなかった。CLIの秘密ファイルは読んでいない。

初見操作の回答待ちのため、全体停止を伴うS6後半はまだ実行していない。exportの成功だけで停止後の継続を達成扱いにしない。

## 本人確認の待機環境

初見操作のHumanReviewは未回答のまま、通常のWeb認証と現在の編集権限で回答できる状態にした。実S3の工程は「準備ができたので開始」の判断待ちであり、sleep用CLIは未起動。実施時刻を一度まとめて本人へ確認している。

準備用の `test:acceptance` には4時間の上限があるため、通常APIへ移した状態を継続できるよう、専用Composeの削除を行う親プロセスを対象確認後に停止し、準備用Go試験も終了した。この準備コマンド全体をPASSとは記録しない。専用DB・Ory・通常API・release Runner・Webは維持した。

操作補助の二アカウントは、同じ既存の合成Ory identityからPKCEで試験トークンを取得する専用loopback transportへ移した。管理用の試験login/consent受理を使い、通常の本人ログイン成功の証拠とは分ける。Workspaceや端末の許可は変更せず、通常APIの `/auth/me` で元と同じ二つのUser IDを確認した。トークンは補助プロセスのメモリ内にだけ保持する。ブラウザの本人用タスクとWindowsのDPAPI認証は維持している。

## 環境と入力

koujou-sanの作業ブランチは `codex/issue-33-kukuri-1887`。kukuriの固定基準は `dea966f6866d9b3978a5a9db1add7f1e3e8e5d5e`（origin/main、0.4.6）。元checkoutは `997653347` のまま変更していない。Codex CLIは0.162.1、Claude Codeは2.1.300。両CLIの既存認証を使い、新しいOAuth・契約・キーは作成していない。

`pnpm run test:acceptance` の専用Ory・PostgreSQL・ArcadeDBとBleveを使用している。MCPの既存結合試験は実Oryと1万Context×3版を通過し、画面用の二アカウントと別Workspace `runner-ui` を用意した。試験用資格情報はGit管理外に保存し、WindowsのOAuth・端末情報はDPAPIで暗号化した。

最初の端末準備は試験環境の起動前に旧fixture metadataを読んで接続に失敗した。起動完了・metadata更新を確認してから再実行し、`issue33-native-auth2.log` と `issue33-native-enroll2.log` が成功した。モデル実行は発生していない。

## worktree作成の未達と修正

最初の `run:kukuri-1887-preparation` はqueuedのまま `local_configuration_missing` となった。実CLIは未起動。同じGit操作を診断し、`issue33-worktree-before.log` でkukuriの既存の視覚baselineに対する `Filename too long` とindex作成失敗を確認した。

既存のworktree分離試験に長い追跡ファイルを加えた `issue33-long-path-before.log` は `git_preparation_failed` でFAIL。WindowsのGit呼出しにだけ `-c core.longpaths=true` を渡した `issue33-long-path-after.log` はPASSとなった。元の編集・branch・commitの保持、再準備での非初期化、二つのworktreeの分離も同じ試験で確認した。保存されたGit設定はfalseのままで、個人設定を変えていない。

未開始の元Runを取消し、修正版debug Runnerで新しい `run:kukuri-1887-preparation-fixed` を実行した。Run専用worktreeと `koujou/run-*` branchを自動作成し、別メンバー（編集者、実行依頼は不許可）が2回回答して、同じRunを再開・成功終了した。質問のrecord IDとCLI内のpause IDを混同していた受入driverは修正した。製品のPause契約は変更していない。`.cache/issue33-native/baseline.json` に固定入力と回答を保存した。

現時点の実行用binaryは修正後にbuildしたdebug Runnerである。配布対象のrelease binary・実スリープを確認したとは扱わない。

## 修正前の独立検品と実装Run

`run:kukuri-1887-inspection-baseline-fixed` は、Claude Codeの新しいsession・別worktree・無注入で実行した。Inspection定義 `inspection:1887-baseline-fixed@WGG3WRNJABF5DWQYNFGCRRJJWG` と空Manifestのdigestを固定し、K-1/K-2をFAIL、全体をFAILとして取得した。CLI自体は終了コード0であり、実行成功と検品の不合格を区別した。

具体的な根拠は、公開スレッドでも降順のtimeline配列をそのままFeedへ渡し、新着を先頭へ追加していたこと、共通ヘッダーにフィルター・ブックマーク表示切替があり、投稿にもブックマーク操作を出していたことだった。sourceの読み取りによる判定で、Vitest・実画面は未実行と報告している。`.cache/issue33-native/baseline-inspection-state.json` に原レポートを保存した。

最初のCodex用ラッパーは、モデル取得用にRunnerが先頭へ加える `-c` をNodeのオプションとして解釈し、provider起動前に終了コード1となった。CLIモデル枠は未消費。受入用のcmd入口でproviderオプションを転送するよう直し、元Runを変更せず新しい `run:kukuri-1887-implementation-fixed` へつないだ。

同Runは新しいRun定義の静的検証・検証記録保存・採用から作成した。作成時はqueued／未割当であり、明示割当後にだけCodexを起動した。前段の検品・失敗した受入接続を固定参照で辿れる。実装用worktreeは `koujou/run-13ed679f009e8936ee0d6a45a73b49e1`。原会話・自己評価は自動注入していない。

## 未完了の確認

### 修正案と比較試験

Codexの実装Runは終了コード0、自己評価INCONCLUSIVEで終了した。型検査と差分検査は成功したが、依存の配置によりVitestのassertionを実行できなかったという原レポートを保持している。後続の機械試験の成功で、この原評価を書き換えない。

受入用OAuthの保存先をリポジトリ内へ置いたため、pnpmが親のkoujou-san workspaceを検出する配置になっていた。完了済みのjournalは保存し、未実行のプロセスがないことを確認して、資格情報と候補worktreeをリポジトリ外の専用cacheへ移した。Gitのworktree移動を使い、branch・commitと編集内容を保持した。依存も短い物理node_modulesへ配置し直した。通常のプロジェクト依存・lockfileは変えていない。

`issue33-kukuri-before3.log` は固定基準commitへ新しい回帰試験だけを適用し、13試験中5件FAIL。`issue33-kukuri-after4.log` は同じ13試験が全PASS。投稿順、新しい投稿と古いページ、フィルター／ブックマーク表示、保存された旧表示の復元を比較した。先行する試験の起動失敗を、挙動のFAIL再現に数えていない。

### 通常APIへの継続

受入のアカウント・データ作成後、模擬の外部backendを持つ画面fixtureのAPIを停止した。専用PostgreSQLのアプリDBを一貫したdumpから別DBへ復元し、Run record 763件・Inspection record 13件・Context版30126件の一致を確認した。既存のOry・二つのactor gateway、API originと試験TLSを維持し、通常のAPI実行ファイルへ切り替えた。通常の認可・実行・Worker・外部backendを使い、模擬結果を実接続成功と扱わない。資格情報・dump・設定はGit管理外である。

通常APIで既存アカウントの認証を確認し、Bleve・ArcadeDBは復元先から再構築する。`issue33-release-runner-build.log` で修正版release Runnerのbuildも完了した。release Runnerによる後続実行と実スリープはまだ確認していない。

専用モデル中継は、20要求・入力32 KiB・出力4096 token・90秒、fallback／自動retryなしの枠を、転送前の消費記録で制限する。要求モデルを固定し、Ory/MCPの認証情報を転送せず、本文は中継ログへ保存しない。試験用の出力上限の付加を記録し、通常の接続設定や製品の初期値は変更しない。まだモデル要求は送信していない。

実装結果、修正後の実表示・再検品、既存CI、条件付きmerge、LLM三機能、サーバー評価とクライアント不在の連鎖、外部起票の応答喪失、費用比較、export、AC-01〜68の証跡対応が残る。人の初見操作と実OSスリープは未確認であり、準備後に実施タイミングを本人へまとめて確認する。#34は保留を維持する。

## 実画面と固定成果物の再検品

共通Feed・scroll anchor・merge・通常タイムラインの64試験、channel・公開スレッドLiveの15試験もPASS。型検査、変更TS/TSXのlint、Storybook buildが成功した。Chromeで1858×947・英語と390×844・日本語を比較し、会話順と不要操作の除去を確認。返信をpointerで開いてCtrl+Enterで送信し、topic/channelを含むリンクをコピーした。画像は[kukuri PR #1890](https://github.com/kukuri-app/kukuri/pull/1890)の公開UI記録へ保存した。

commit `ffbf360e69293500a5882b4386552a105534dcd4` のInspectionRun `run:kukuri-1887-inspection-corrected` は、92試験と型検査を別worktreeで実行して原レポートPASSとなった。ただし、受入driverが検証の説明までunverifiedへ書くよう求めたため、製品の保守的な採用判定はINCONCLUSIVEのまま保持した。説明は通常の活動記録へ出し、unverifiedは条件の未確認だけに使うよう、受入用入力を訂正した。原レポートは書き換えていない。

CIでK-2に必要な共通判定のimport一行が行数基準を超えたため、REFACTORING.mdに従いbaselineを1138→1139へ再生成した。機能の分割や行の圧縮で見かけの数を減らしていない。commit `d1371fe9add3fe60270e3bb3fd16f778a37ca8f7` の `run:kukuri-1887-inspection-accepted` は、三条件すべての実行・証拠・未確認を照合して、原レポートと製品の採用判定の両方がPASS。固定resultは `run-result:kukuri-1887-inspection-accepted@NZZ5NBOJAT6EV4L7JD6WOZGKOY`。

その後の既存ブラウザ試験では、Live開始投稿を上端から流す従来の降順を前提にしていた一件がFAILとなった。会話順では開始投稿が上にあるため、下端へスクロールしてもヘッダーからLiveへ進める同じ目的の試験へ修正した。同じPlaywright試験は1件PASS、7秒（`issue33-kukuri-live-browser-after2.log`）。初回ローカル実行のChromium未配置は、assertionのFAILと区別した。最終候補は `82ff9f50d37b89ad8c7b58b9ba1baadb2a719b8d`。新しい対象版へ旧PASSを流用せず、新InspectionRunを用意した。

## LLM三機能と手動編集

通常のWeb認証で専用の編集者を招待し、実行依頼は不許可のまま使った。MCPも通常のOAuthで接続した。実OpenAI APIをBifrost経由で使い、モデルは各応答の `gpt-6.1-sol` を確認した。

- Context選択: 3要求。検索・固定文書の取得から `document:1887-request@7RVUMRKY4SOL3GIFPLPOOVIVSQ` を保存し、理由と固定版を注入プレビューへ反映。Runは未作成。
- 定義生成: 合計11要求。モデルのrunner接頭辞付きdevice引数と不適切なqueryをMCPが拒否し、上限到達も保留として記録した。既存toolの引数説明を明確にした後、実接続情報からprompt案を取得。担当端末未設定で自動採用は保留され、同じ定義へ端末を手動補完し、静的検証・検証記録保存・明示採用を確認した。`run-definition:a64ac8dc-8cc7-4e86-b9a3-3f771ea4fea5@IGDBKQV3BUAZBAVVH2FU36UUJN`。保存・採用からRunは始まっていない。
- outputからのContext生成: 2要求。固定出力 `run-output:kukuri-1887-implementation-fixed:81@OQ77XZ2SASTZPBM7XOJ5JJP23W` を一回取得。モデルがContextのreferencesへRun出力参照を混ぜた応答は拒否し、本文を保って参照形式を手動修正した。同じ操作の新しい案を採用してSentenceを作成し、通常の編集画面で新版を保存した。画面は「生成後に手動で変更」を示し、元Run・出力版・生成操作・digestを保持した。元出力の自己申告を独立PASSへ変えていない。

生成したSentenceは `generated-c2ff2ab69c291d4a5bdf5b8db3dcb871346c5a9f11197573aa690ec4d78d8c81`、生成版は `YIY2FHV7CZ47CP2W66IULGC5YV`、操作は `client-operation:f2b6a62d-550f-43cd-9842-cfb87491903d`。手動版と原出力は別の固定版として読み取れる。Web再読込みでモデルキーが失われる方式も維持した。三機能が毎回自動採用されたとは扱わず、拒否・保留・手動復帰を記録した。生成用system文にも、referencesは既存Contextの固定参照だけで、元出力の由来はサーバーが付けることを明記した。

## 通信断・起票応答喪失・サーバー評価

release Runnerの実検品中、所有APIだけを停止した。Runner PID 87908、CLI PID 81804は停止前後で同一。切断中のRunnerはconnected=false・active=1・pending_frames=trueとなり、CLIは同じ開始時刻のまま継続した。API再開後はconnected=true・pending_frames=falseとなり、そのInspectionRunが正常終了した。実OSスリープの代替とは扱わない。`outage*.json` と `issue33-api3.log` に観測を保持した。

既存の書込みAppで[合成Issue #69](https://github.com/KingYoSun/koujou-san/issues/69)を一件作成した。試験用transportで実際のGitHub作成応答だけを破棄し、共有要求がUnknown / create_response_lostになることを確認。再処理とHTML照合キーによる取得で同じIssueへ対応付けた。POST作成は一回、別Issueへの書込み・新ラベル作成・再Open・本文編集はなし。その一件だけをCloseして終了した（`issue33-publication-loss-live.log`、PASS）。認証・失敗時の回数も消費する一件限定receiptをGit管理外へ残した。

サーバーHopperは基準検品の固定報告を一要求で意味評価し、元のcommitにおけるK-1/K-2の疑いを正確な引用付きで記録した。現行branchが不具合のままとは断定せず、過去へのdry-runとして追加発行しなかった。Triageは一要求で専用IssueのP2を採用し、未計測の影響・工数を創作していない。原入力・原応答・実モデル・usageを保存し、料金未設定はmissingのまま。`hopper-evaluations.json`、`triage-jobs-after.json`、モデル中継のmetadataログへ保持した。

2026-10-19 15:14:25 UTC、受入用Webタブを全て閉じ、active=0を確認してrelease Runnerも終了した。残る製品プロセスは0件。サーバー設定の一時停止を通常APIで明示解除し、Triageを通常のresume操作で再開した後は、画面やRunnerを動かさず結果を取得した。CI後のHopperは、固定した旧版CIの失敗・中断を契機に、一件の確認用Runだけを発行する設定。追加の外部Issueは作らない。発行・Runner待機・再接続の観測は継続中である。

実接続の現在消費はCLI 4/6回、API 18/20要求。APIはContext選択3、定義生成11、出力Context生成2、Hopper1、Triage1。承認枠をリセットせず、残る2要求は未使用。失敗要求・保留・料金未設定を成功や0費用へ補完しない。

[全68条件の対応表](2026-10-19-issue-33-ac-matrix.md)に既存の有効な機能証拠と今回の範囲をまとめた。CIと条件付きmerge、比較、export/停止後継続、人の初見操作、実スリープと最終の独立受入検品は、完了するまで未確認・待ちのまま残す。

## 無クライアント連鎖と再接続の確認

旧Fast CIのjobs一覧APIが502となった間は、観測をunknown / source_unavailableとして保持し、完了や成功へ補完しなかった。同じ最終commitの既存監査CI [39277852977](https://github.com/kukuri-app/kukuri/actions/runs/39277852977)を、run ID・commit・successの三条件で絞って登録した。画面とRunnerを閉じた後に通常のサーバーWorkerが実際の完了を受信し、Run `run:hopper-ecbf1e75f23c4e4a083b374ba53f373ae001dc90fda819f5c8b76e39886141fd` を一件発行。P2とrunner_unavailableを、started_atなし・assignmentなしで記録した。15:40:21 UTCのCI観測を含む `clients-absent-results.json` に、全製品クライアント不在と同じ固定参照を保存した。

再接続すると同じRunへ割当が付き、別メンバーの二回答から同じRunが正常終了した。別の旧版CI中断から生じたRunも保存されているが、定義更新でdefinition_or_controller_changedとして保留され、CLIは未起動のまま。別起点の二つの履歴を同じ発行の重複とは扱わず、保留を完了へ変換しない。

最終commit `82ff9f50d37b89ad8c7b58b9ba1baadb2a719b8d` のInspectionRunも、製品の採用判定までPASS。固定resultは `run-result:kukuri-1887-inspection-final@KKVZ45T4THX5IDKQPPKCLDQEQR`。同じ旧PASSを新commitへ使う実工程では、照合RunがINCONCLUSIVE、mergeノードがskipped、merge用Runが未作成となった。CLIの終了コード0を検品PASSへ読み替えていない。

## 比較画面

`comparison:1887-inspection@Q2B2QFUMYP2EWTMCUDZOU7B7OV` は、同じ期間・claude-fable-5-1・無注入・三条件の基準版/修正後を固定テンプレート版で絞った。画面で独立FAIL/PASS、実行/待機時間、取得範囲と未算定を並べ、CLI自己申告と独立結果を区別した。基準側はsource検品、修正側は92試験・対象のブラウザ試験も実行した違いを、原検品記録から確認できる。

`comparison:1887-coverage@AM2PVNPZAJAHNTTE6OF7PNICJJ` は、同じ期間の利用量/API換算額を帰属別に確認した。API 18要求は操作16・Hopper1・Triage1の計測に一致し、元Runへ加算していない。操作16件の提供元合計と、既知cache/推論内訳を含む投影の合計も一致した。推論内訳12件の欠測は残す。Codexのgpt-6-astraとClaudeの実モデルを記録し、起動前に失敗した接続のモデル未取得も保持した。定額契約・単価未設定の金額は未算定で、0円ではない。費用優劣や節約率は主張しない。画面とJSONは `comparison-*.jpg/json` に保持した。

実接続はCLI 5/6回・API 18/20要求。残る枠は使っていない。本人用HumanReviewRunは固定成果物・役割・質問を用意して未回答で待機。実スリープ用工程も、本人の開始時刻の判断までCLIを作らない状態で待機している。
