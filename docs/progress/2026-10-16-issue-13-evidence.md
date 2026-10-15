# Issue #13 検証記録

対象は[計画](2026-10-16-issue-13-plan.md)、基準mainは`bb0c5cbb26250f2bb56b41193de91a20b32d7505`。Scope revision 1、区分C。現在の判定は[current-state](current-state.md)へ集約する。

## 保存と外部接続

GitHub Appを初期方式とし、定期照合の初期値60秒（変更可能）、変更があった観測履歴の期限なし保持はユーザー承認済み。追加migration 010で外部対象・Workspaceの対応・現在観測・変更履歴を保存する。同一対象の複数参照をまとめ、対象・履歴・ジョブをページで取得する。Context本文の全件走査は行わない。

App「koujou-san-kukuri-observer」をkukuri-app所有で非公開登録し、kukuri-app/kukuriだけへインストールした。Actions・Contents・Issues・Pull requests・MetadataはRead-only、Webhookは無効。App作成・秘密鍵生成・限定インストールは実行直前にユーザー承認を得た。[インストール証跡](assets/issue13-github-app-installed.jpg)。App IDは5336407、installation IDは171862620。秘密鍵生成後の自動ダウンロードはChromeが`ERR_BLOCKED_BY_CLIENT`で拒否し、鍵の取得を本人へ引き継いだ。秘密値を取得済みとして扱わず、実接続試験はこの時点では未実施。

## 条件と現在の証拠

| 条件 | 実行・照合した対象 | 現在の結果 |
| --- | --- | --- |
| AC-1 | 四種類の固定応答、実Appの最小権限・限定インストール、共通UIの実装 | 実Appによる取得と実画面照合は鍵取得後に行う |
| AC-2 | 専用PGと固定応答。新しい回の成功後に重複・遅延・逆順の古いfailure通知を投入 | 現在状態を再取得し、同じ内容の照合では履歴を追加しない |
| AC-3 | 同じrun IDの1回目failure、2回目success、指定と異なるcommit、必要ジョブのskip、固定した1回目 | ID・SHA・workflow・attempt・ジョブ結論を分離。UIの35件目の試験でsuccessをPASSへ置換しないことも照合 |
| AC-4 | 接続先503、401、ジョブ取得2ページ目の失敗、API残量/Retry-After | 最後の結果・成功取得時刻を保持し、現在の観測はUnknown。部分ジョブを確定せず、API制限では減速 |
| AC-5 | 専用PGのWorkerだけを動かし、クライアント不在で新しい回を観測して完了へ更新。再起動後に再取得 | 履歴と同じ完了結果を保持。実Appの固定Actions照合は後続 |
| AC-6 | 署名JWT・repo IDを限定したread token要求、他Workspace・制限resource、範囲外repo ID・名前不一致・未署名Webhook | 拒否を確認。GitHubへの業務書込みなし。実接続の要求記録は後続 |
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

## 残る検証

明示したAppによる実GitHub取得・要求記録、Web/Tauriの操作、固定成果物の独立検品、実PRのCI、マージ後照合を残す。通常CIは固定応答と専用DBを使用し、実Appの秘密鍵を要求しない。未実行を成功に含めない。
