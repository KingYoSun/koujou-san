# Issue #4: 共通保存と実DB検証の記録

- 日付: 2026-10-15（JST）。基準: `c478dd901a3414d9ce824ba9586fe98fa997ae84`。
- 対象: [Issue #4](https://github.com/KingYoSun/koujou-san/issues/4)、Scope revision 1。
- 現在の判定: [現在状態](current-state.md)を参照。

## 変更

pgx v5.11.0でGo APIをPostgreSQLへ接続し、初期SQL migrationと共通の版・stream・更新記録・操作結果を実装した。業務serviceのcallbackを同じtransactionへ渡す。Domain schema・Ory認証・投影・同期は各Issueへ残し、試験専用CRUDと仮の認証主体を製品へ公開していない。

Go APIの実プロセスを18088番で起動し、標準PostgreSQLへ接続・migrationした。healthzは従来のstatus/service/stageを保持し、readyzはstatus okを返した。Ctrl+Cで停止した。DB未設定・到達不能のreadyzとhealthzの分離はHTTPテストで照合した。

## 実DBの条件照合

Windows 11のDocker上でpostgres:17.11-bookwormを使い、test:databaseから毎回専用project・volume・loopbackポートを作った。DDL、業務更新、版、更新記録、結果保存は実PostgreSQLで実行し、SQL mockへ置き換えていない。

| 条件 | 結果                                                                                                                                                                                                                                  |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| AC-1 | 空DBとbootstrapの既存fixtureを含むDBで初期schemaが一致。再実行はmigration記録1件と既存データを保持。途中SQL失敗で新規tableとmigration全体がrollback。適用済みSQLの編集を拒否                                                          |
| AC-2 | 更新記録へのINSERTをDB triggerで失敗させ、業務payload・版・更新記録・receiptが全て旧状態/件数を維持                                                                                                                                   |
| AC-3 | 並列HTTPの同一操作再送は同一結果で記録追加0。同一版への別更新は成功1・version_conflict1。操作内容変更はoperation_conflict。commit後にHTTP接続を落とし、再送結果と保存済みreceiptが一致                                                |
| AC-4 | 作成・更新・削除の版と位置をページで照合。ページ間の追加更新を既存範囲へ混ぜず次の範囲で取得。API試験processを別PIDへ再起動してcursorと再送を継続                                                                                     |
| AC-5 | 二つのWorkspaceでIDの差替えを拒否。別Workspaceへの保存/読取、読取権限だけの更新を拒否。操作IDはActorにも束縛。同じActorの更新権限取消後は保存済み操作の再送も拒否                                                                     |
| AC-6 | 試験PostgreSQLを実際に停止し、更新成功とreadyを返さないことを確認。DB/API再起動後のreceiptが一致。pg_dump/pg_restoreでデータ・版・位置・結果を照合。復元後の別更新で古い位置を流用せず、欠落位置・記録の穴をhistory_unavailableとした |

通常check、test（GoとUI 12テスト）、buildは成功。実DB試験は明示コマンドで実施し、通常testでskipされた実DB条件を成功へ補完していない。nativeコードは変更していない。実際のCIと独立検品、マージ後照合は別に確認する。

## 発見と修正

最初の実DB試験で再送結果の不一致を検出した。失敗応答で初期化したGo structへreceipt JSONを読み込むと、JSONに無いerrorやreasonが残ったためだった。再送の復元先をゼロ値へ戻し、同じ並列・応答喪失の試験が通過した。

次の試験ではポート0を使うcontainer構成で停止・開始後の接続を再開できなかった。試験起動時に一つの空きportを選び、Composeへhost portを明示する構成へ変更し、同じ停止・復元の試験が通過した。アプリの再送・更新処理をこの問題のために変更していない。

試験終了時に専用container・volume・networkの削除を確認した。標準の `koujou-san-dev-postgres-1` は同じID `179cadb9319c`、15432番、healthyのままだった。試験の停止・復元は標準DBやOry内部DBへ行っていない。
