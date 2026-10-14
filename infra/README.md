# 開発用サービス

リポジトリの root から `pnpm run setup`、`pnpm run infra:up` の順に実行する。Compose project は `koujou-san-dev` に固定し、公開するポートはすべて `127.0.0.1` に限定する。

| サービス              | 接続先                                            | 固定 image                    |
| --------------------- | ------------------------------------------------- | ----------------------------- |
| PostgreSQL            | `127.0.0.1:15432`                                 | `postgres:17.11-bookworm`     |
| ArcadeDB              | `http://127.0.0.1:2480`                           | `arcadedata/arcadedb:26.10.1` |
| Kratos public / admin | `http://127.0.0.1:4433` / `http://127.0.0.1:4434` | `oryd/kratos:v26.2.0`         |
| Hydra public / admin  | `http://127.0.0.1:4444` / `http://127.0.0.1:4445` | `oryd/hydra:v26.2.0`          |

`pnpm run infra:status` で状態を確認し、`pnpm run infra:down` で停止する。通常の停止は named volume を保持する。PostgreSQL は `koujou`、`kratos`、`hydra` を別の role・database に分け、Ory の migration が成功してから各サービスを起動する。

この段階の確認対象はサービスの起動と readiness である。ログイン・同意画面、OIDC client 登録、API の認可、Workspace 権限、製品データの migration は今後の実装で扱う。Ory の UI URL は共通クライアントの `127.0.0.1:1420/auth/*` を予定する設定であり、画面はまだ実装されていない。Bleve は Go に組み込む検索ライブラリなので、Compose のサービスには含めない。

`.env` は setup が初回に秘密値を生成し、既存ファイルを保持する。PostgreSQL の初期化スクリプトは空の volume に対して一度だけ動く。データを保持したまま `.env` の database password や Ory の暗号鍵を書き換えると既存の保存内容と一致しなくなるため、変更時は対応するサービスの移行手順を先に定める。volume の削除は通常の停止に含めない。

設定の構文だけを確認する場合は次を実行する。秘密値を出力する通常の `compose config` は使わず、`--quiet` を付ける。

```sh
docker compose --env-file .env.example -f infra/compose.yaml config --quiet
```

参照: [PostgreSQL official image](https://hub.docker.com/_/postgres)、[ArcadeDB の Docker 設定](https://github.com/ArcadeData/arcadedb/blob/26.10.1/docker-compose.yml)、[Kratos の設定例](https://github.com/ory/kratos/blob/v26.2.0/contrib/quickstart/kratos/email-password/kratos.yml)、[Hydra の設定例](https://github.com/ory/hydra/blob/v26.2.0/contrib/quickstart/5-min/hydra.yml)。Ory は通常の OSS image を使用する（[Kratos license](https://github.com/ory/kratos/blob/v26.2.0/LICENSE)、[Hydra license](https://github.com/ory/hydra/blob/v26.2.0/LICENSE)）。
