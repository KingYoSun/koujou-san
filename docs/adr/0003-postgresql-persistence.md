# ADR 0003: PostgreSQLの共通保存と更新位置

- 状態: Accepted。
- 日付: 2026-10-15（JST）。
- 対象: [Issue #4](https://github.com/KingYoSun/koujou-san/issues/4)、設計書§6.1・6.3・6.6・11.1・11.2、[ADR 0002](0002-shared-api-contract.md)。

## 判断

PostgreSQL専用のpgx v5.11.0とpgxpoolを使う。ORMや別のmigration実行ツールは導入せず、番号付きSQLをGoへ埋め込む。導入時のDDLとmigrationのdigest記録を同じtransactionで確定し、advisory lockで並行起動を直列化する。適用済みSQLの変更は拒否し、変更が必要なら後続migrationを追加する。今回の旧版はbootstrapのアプリschema未導入状態であり、初期版の再実行も検証する。

`shared_streams`、`shared_versions`、`shared_changes`、`shared_receipts`に共通の更新位置・版・更新記録・操作結果を保存する。Domainテーブルは各機能が追加する。`Store.Apply`のcallbackはそのDomainテーブルを同じtransactionで更新し、投影用に確定後の内容を返す。APIからBleve・ArcadeDBへ個別に書き込まない。

Workspaceごとのstream行をロックし、再送結果の照合、前提版の比較、Domain更新、版・更新記録・結果保存を一つのtransactionで処理する。同じWorkspaceの内部連番がcommit順になるため、未commitの小さな番号を後から読飛ばすことがない。グローバルなsequenceや端末の時計で更新順を確定しない。

安定IDはDomainが渡し、版と更新位置はサーバーが生成するランダムなopaque文字列とする。未作成IDの前提版は文字列 `"0"`。版は等価性の判定に使う。更新記録にはWorkspace内の連番と削除のtombstoneを残し、位置の文字列から連番を推測しない。backupから分岐して同じ連番を再使用しても、異なるイベントの位置が一致しない。

要求digestはJSONのobjectキーと空白を正規化して、対象ID・種別・前提版・削除指定・payloadからサーバーが生成する。数値はJSONの数値表現を保持し、浮動小数点へ変換しない。操作結果はWorkspace・認証済み主体・操作IDの組で保存する。現在の対象権限を業務serviceが導出し、読取/更新を確認してから結果を返す。認証主体の導出と権限のDB照合は#5・#6が担当する。

## 読取と保持

更新記録の読取はrepeatable readのtransactionで取得範囲を固定し、Workspaceと連番の索引で必要なページだけを読む。cursorはWorkspace・基準位置・続きの位置を保持する。next_cursorを使う間の新規更新は、固定した基準位置以降の次の範囲に現れる。cursor自体はアクセス権を与えない。

この初期版では更新記録と操作結果を自動削除せず、時間によるcursor期限を置かない。位置が失われた、復元先に無い、範囲内の連番に欠落がある場合は、dataを返さず `unavailable` と `pending / history_unavailable` を返す。storage停止は `pending / storage_unavailable` と区別する。snapshotの再取得とUIの切替は#8が実装する。保持期間を有限にする運用は後続の判断とし、ここで削除機構を先取りしない。

## 起動と復旧

Go APIは設定されたDBへの接続・migrationが失敗すれば起動を終了する。既存 `/healthz` の形は維持し、`/readyz` は設定済みDBの到達を確認する。DB未設定や停止をreadyにしない。更新のcommitを確認できない場合も成功を返さず、再送時に永続的な操作結果を照合する。

backupはDomainテーブルと共通の版・stream・更新記録・操作結果・migrationを同じDBのpg_dumpで保存する。復元先を新しいDBとして作り、復元に成功してから接続先を切り替える。位置の欠落を成功や空の全件取得へ補完しない。手順は[保存と復旧](../runbooks/persistence.md)、実証は[Issue #4の記録](../progress/2026-10-15-issue-4-evidence.md)を参照する。

## 参照

- [pgx v5.11.0](https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0): PostgreSQL専用interface、JSON型とtransaction。
- [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0/pgxpool): pool生成後の接続確認。
- [PostgreSQL 17のロック](https://www.postgresql.org/docs/17/explicit-locking.html): row lockとtransactionの関係。
- [PostgreSQL 17のbackup](https://www.postgresql.org/docs/17/backup-dump.html): pg_dumpと復元。
