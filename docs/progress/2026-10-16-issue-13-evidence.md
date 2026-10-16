# Issue #13 検証記録

対象は[計画](2026-10-16-issue-13-plan.md)、基準mainは`bb0c5cbb26250f2bb56b41193de91a20b32d7505`。Scope revision 1、区分C。現在の判定は[current-state](current-state.md)へ集約する。

## 保存と外部接続

GitHub Appを初期方式とし、定期照合の初期値60秒（変更可能）、変更があった観測履歴の期限なし保持はユーザー承認済み。追加migration 010で外部対象・Workspaceの対応・現在観測・変更履歴を保存する。同一対象の複数参照をまとめ、対象・履歴・ジョブをページで取得する。Context本文の全件走査は行わない。

App「koujou-san-kukuri-observer」をkukuri-app所有で非公開登録し、kukuri-app/kukuriだけへインストールした。Actions・Contents・Issues・Pull requests・MetadataはRead-only、Webhookは無効。App作成・秘密鍵生成・限定インストールは実行直前にユーザー承認を得た。[インストール証跡](assets/issue13-github-app-installed.jpg)。App IDは5336407、installation IDは171862620。秘密鍵生成後の自動ダウンロードはChromeが`ERR_BLOCKED_BY_CLIENT`で拒否し、鍵の取得を本人へ引き継いだ。秘密値を取得済みとして扱わず、実接続試験はこの時点では未実施。

## 条件と現在の証拠

| 条件 | 実行・照合した対象 | 現在の結果 |
| --- | --- | --- |
| AC-1 | 四種類の固定応答、実Appの最小権限・限定インストール、実GitHub取得、Web/Tauri | 取得元・識別子・対象版・時刻を実APIと画面で照合 |
| AC-2 | 専用PGと固定応答。新しい回の成功後に重複・遅延・逆順の古いfailure通知を投入 | 現在状態を再取得し、同じ内容の照合では履歴を追加しない |
| AC-3 | 同じrun IDの1回目failure、2回目success、指定と異なるcommit、必要ジョブのskip、固定した1回目 | ID・SHA・workflow・attempt・ジョブ結論を分離。UIの35件目の試験でsuccessをPASSへ置換しないことも照合 |
| AC-4 | 接続先503、401、ジョブ取得2ページ目の失敗、API残量/Retry-After。専用DBの保存状態fixtureで両画面も照合 | 最後の結果・成功取得時刻を保持し、現在の観測はUnknown。部分ジョブを確定せず、API制限では減速 |
| AC-5 | 専用PGでクライアント不在・完了更新・再起動。実AppでWeb/Tauri終了前後の再取得と再接続 | 5対象を画面終了後も再取得し、同じ内容版を再接続後に取得 |
| AC-6 | 署名JWT・repo限定read要求、他Workspace・制限resource、範囲外repo ID・名前不一致・未署名Webhook、実App要求記録 | 拒否を確認。実Appの業務要求はGETだけで、POSTはtoken交換のみ |
| INVAR-1/2 | 取得元・取得時刻・Unknown、CI conclusionと指定commit・必要ジョブを別表示 | 固定応答と保存・表示経路を照合。工程の自動前進・Run生成は追加していない |

## 実行結果

- `pnpm run check`、`pnpm run test`、`pnpm run build`は成功。共通UIは35件成功。
- `pnpm run test:github`は専用PGで成功。最後の実行は2.40秒（Go試験本体）。Compose project `koujou-san-db-test-aa9ca1632dfb`は片付け済み。
- `pnpm run test:database`はmigration・保存・復旧の試験が成功（3.83秒）。専用Compose project `koujou-san-db-test-8512ea951a88`は片付け済み。
- `pnpm run check:desktop`、`pnpm run test:desktop`は成功。4件成功・既存の対話専用5件ignore。
- `pnpm run build:desktop`は成功。Windows実行ファイルを生成した。既知のPDB出力名衝突とlinkerの警告は残る。
- `go test ./internal/githubobserve ./internal/mcpapi`は、実UI検証の接続準備とrepo ID/名前照合の修正後にも成功した。

途中の失敗は、Go整形漏れ、nativeの既存`Url`再公開を使わず未依存の`url::Url`を参照したコンパイルエラー、PG試験で「変更なし再確認」の成功取得時刻を古い時刻と比較したfixtureの誤り。いずれも修正後に関係する検証を再実行して成功した。

AC-6のrepo IDと名前の対応には、修正前に`repository ID/name mismatch accepted`で失敗する試験を追加した。token発行応答のrepository IDとfull_nameを照合し、token再利用時も名前を照合する修正後に同じ試験が成功した。範囲外repoの403が後続の中断試験をrate limit待機で代替しないよう、拒否試験のRemoteを分け、2ページ目の失敗理由もassertした。

PRの現在状態には、GitHubの`merged:true`を受けても`closed`だけを返す不足があった。修正前に`merged PR was displayed as an unmerged closed PR`で失敗する試験を置き、`merged`を区別する修正後に成功した。`merge_commit_sha`はマージ前には検証用の値となるため、その意味も共通UIへ明記した。

## 残る検証

記録を含む最終headのCI、マージ後照合を残す。通常CIは固定応答と専用DBを使用し、実Appの秘密鍵を要求しない。未実行を成功に含めない。

## CIと先行の独立検品

コード対象は`18c26e7bb8157fce88cdd605e8de26514137f2ce`。[CI 38803132388](https://github.com/KingYoSun/koujou-san/actions/runs/38803132388)はLinux 4分43秒、Windows 7分32秒、GitGuardianが全て成功した。同じコードからWindowsアプリを再ビルドし、25.85秒で成功した。

[先行の独立検品](2026-10-16-issue-13-inspection.md)では具体的blockerは0件。実Appの接続と実画面の証拠がない条件はINCONCLUSIVEのまま残し、マージ可能とは判定していない。Appの秘密鍵ファイルが所定のGit管理外パスへ保存された後、準備済みのlive/UI検証を実行して最終判定する。

## 実App・実画面の結果（2026-10-16の再開後）

本人が秘密鍵を保存した後、`pnpm run test:github:live`が10.80秒で成功した。[要求記録](assets/issue13-live-requests.json)にはAppによるtoken発行201と、kukuriのIssue #1220・PR #1843・commit・Actionsへの9件のGETだけを記録した。署名鍵やtokenの内容は含めない。run 38784153986 / workflow 248011545 / head ec3a86cf6a02bfd97079fd7a2e918afd06096c3cの1回目failure・2回目successを区別し、名前が一致しない範囲外repoはGET前に拒否した。

`pnpm run test:github:ui`も成功した。実Ory・専用PG・実Bleve/ArcadeDBと実Appで`TestMCPIntegration`が776.90秒で完了し、専用Compose project `koujou-san-db-test-e2700fe86c1b`を片付けた。1万Context×3版・100Scopeを含む検証環境で、Web/Windowsの共通画面から同じ観測結果・固定版・時刻を確認した。[共有API](assets/issue13-shared-api-observations.json)、[Webの1回目](assets/issue13-web-observations.jpg)、[2回目とcommit](assets/issue13-web-current-attempt.jpg)、[Windows](assets/issue13-native-observations.jpg)を照合した。Windowsの「GitHubで開く」は、対象runの[実GitHub画面](assets/issue13-github-source.jpg)へ移動した。

画面を開く前からサーバーに完了結果が保存されていたことを[初回の記録](assets/issue13-server-only-observation.json)で確認した。その後Web/Tauriを両方終了し、[終了後の記録](assets/issue13-after-clients-closed.json)で5対象全ての成功取得時刻が終了時刻より後へ進み、内容版は1のままであることを確認した。再接続したWeb画面と[API応答](assets/issue13-reconnected-observations.json)は、終了中にサーバーが保持した同じsnapshot・版を返した。

実Appを失効させる操作は行っていない。外部障害の発生は固定応答・PG試験で確認し、実画面には専用DBの`reason=connection_failed`という保存状態fixtureを入力した。[前後比較](assets/issue13-controlled-unknown.json)でsnapshot・成功取得時刻・内容版が変わらないことをassertし、[WebのUnknownと履歴](assets/issue13-web-unknown-history.jpg)、[WindowsのUnknown](assets/issue13-native-unknown.jpg)を確認した。最後にfixtureの理由を解除して実Appの正常照合へ戻した。

[Web要求集計](assets/issue13-ui-request-summary.json)は674件、観測一覧GET 41件と履歴GET 2件を含み、実行の要求は0件。ネイティブ側も同じ認可付きAPIで取得し、外部ページを開く以外のCLI/CI操作を行っていない。対話専用のnative準備試験は明示選択して成功し、検証用OAuthだけをDPAPIで保存した。通常環境の資格情報・OSの信頼設定は変更していない。

追加証跡commit `a799ecc8006fbbb3901f98460bf5d9573aa9d1a2`を独立検品へ渡し、全AC・INVARはPASS、blocker 0件で終了した。検品後の差分は判定記録と現在状態の更新だけであり、コードや条件は変更していない。
