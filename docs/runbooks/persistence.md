# PostgreSQLの保存と復旧

契約と採用判断は[ADR 0003](../adr/0003-postgresql-persistence.md)、作業開始・終了は[Issueの手順](issue-lifecycle.md)を参照する。Domainテーブルと共通の版・更新記録・操作結果はkoujouのDBへ保存し、Kratos/Hydraの内部DBを直接更新しない。

## 開発時の起動

rootの `.env` をsetupで準備し、PostgreSQLを起動してからGo APIを開始する。

```powershell
docker compose --env-file .env -f infra/compose.yaml up -d --wait postgres
pnpm run dev:api
```

dev:apiは、接続先の明示がなければ `.env` のKOUJOU_DB_PASSWORDを使い、loopbackの15432番、koujou role/databaseへ接続する。別の配置では `KOUJOU_DATABASE_URL` を環境変数で明示する。接続情報と秘密値をGitへ保存しない。外部配置では接続先に合うTLS設定を使う。

DB接続とSQL migrationは起動時に実行する。接続・migration失敗ではAPIを開始しない。DB URLを設定せずバイナリだけ起動した場合は、healthzが生存確認を返してもreadyzは503となる。起動済みAPIでもDB停止中のreadyzは503。共有更新の保存に失敗した結果を成功へ補完しない。

migration SQLの既存ファイルを編集せず、後続番号のSQLと実DBの更新試験を追加する。初期版はアプリschemaが無いbootstrap DBと導入済み初期版に対応する。

## 実DBの検証

```powershell
pnpm run check
pnpm run test
pnpm run test:database
pnpm run build
```

通常のtestはDockerを必須にせず、実DB試験はtest:databaseで明示する。このコマンドは標準Composeと `infra/compose.test.yaml` から毎回別projectを作り、選んだloopbackポートを停止・開始の間も維持する。試験用containerのproject labelを照合し、停止・復元・最後のvolume削除をそのprojectへ限定する。既存の開発DBは停止・復元・削除しない。

空DB/bootstrap/再実行、migration失敗、業務更新途中のrollback、並列HTTP更新・再送・応答喪失、Workspace拒否、削除・複数ページ、API試験processの再起動、PostgreSQL停止・復元を検証する。HTTPの業務routeと認証主体はtest binary内のfixtureであり、製品APIには公開しない。CIのLinux jobもこの実DB試験を実行する。

## backup

PostgreSQLのDB単位で取得し、Domainデータと共通の版・更新位置・操作結果を一緒に保存する。以下は標準開発containerの例。バックアップは実データを含むため、Git管理外の保存先を使う。

```powershell
$koujouBackupName = 'koujou-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '.dump'
New-Item -ItemType Directory -Force .cache/backups | Out-Null
docker compose --env-file .env -f infra/compose.yaml exec -T postgres pg_dump -U koujou -d koujou -Fc -f "/tmp/$koujouBackupName"
docker cp "koujou-san-dev-postgres-1:/tmp/$koujouBackupName" ".cache/backups/$koujouBackupName"
```

PowerShellの文字列pipelineへcustom形式のbinaryを通さず、container内でファイルを作ってコピーする。Oryの内部DBはこのアプリDBのdumpに含まれない。標準構成全体の復旧は#29が扱う。

## 復元

新しいDB名を選び、既存DBへ上書きしない。以下は `koujou_restore` がまだ存在しない場合の例。

```powershell
docker cp ".cache/backups/$koujouBackupName" "koujou-san-dev-postgres-1:/tmp/$koujouBackupName"
docker compose --env-file .env -f infra/compose.yaml exec -T postgres createdb -U postgres -O koujou koujou_restore
docker compose --env-file .env -f infra/compose.yaml exec -T postgres pg_restore -U postgres --single-transaction --exit-on-error -d koujou_restore "/tmp/$koujouBackupName"
```

復元成功後、そのDBを指すKOUJOU_DATABASE_URLでAPIを起動する。保存済みDomainデータ、版、streamの位置、操作結果を元のbackup取得時点と照合する。元DBやbackupは確認前に削除しない。

backupに無い更新位置や、範囲内の記録が欠けたcursorはhistory_unavailableとなる。呼出元はsnapshotを再取得して基準を合わせる。更新記録と操作結果はこの版では自動削除されず、cursorは時刻だけで失効しない。
