# 認証の開発手順

標準Ory Kratos・Hydra、PostgreSQL、Mailpitを使う。採用方式と失効範囲は[ADR 0004](../adr/0004-ory-authentication.md)を参照する。Workspace参加と端末登録は別の機能である。

## 起動と初期管理者

rootで`pnpm run setup`を実行し、`pnpm run infra:up`で標準サービスを起動する。setupは既存.envを保持し、不足する認証暗号鍵・Web client secretを生成する。`.cache/tls/`に開発専用の証明書を作るが、OSの信頼設定には登録しない。

初期管理者のパスワードをGit管理外のローカルファイルへ保存し、次を実行する。実際のメールアドレスとファイルパスを指定する。

```powershell
pnpm run auth:bootstrap --admin-email admin@example.test --password-file .cache/admin-password.txt
pnpm run dev:api
```

別のターミナルで`pnpm run dev:web`を起動する。`https://localhost:1420`を開き、表示された開発用証明書を本人が確認する。初期管理者も「メール確認」から確認コードを受け取る必要がある。ローカルのテストメールは[Mailpit](http://127.0.0.1:18025)で読む。bootstrapの再実行は同一Identityを明示的に管理者へ対応付けるが、既存パスワードを再設定しない。

自己登録したアカウントはメール確認後にログインできる。「アカウント設定」で表示名・パスワード・任意TOTPを設定する。「パスワードを忘れた場合」はメールコードで設定画面へ復帰する。登録済みTOTPがある場合は第二要素も必要になる。

Windowsは`pnpm run dev:desktop`を使う。Viteの1420番ポートを共用するため、既に`dev:web`を起動している場合はそのプロセスを先に停止する。OAuthは外部のシステムブラウザで行い、既定では`127.0.0.1:19487/callback`へ戻る。長期資格情報はアプリのローカルデータディレクトリ内の`auth-<接続先hash>.dpapi`へWindows CurrentUserで暗号化して保存する。ログアウトで自身のファイルを削除する。

## 設定

Goの認証を有効にする場合は`KOUJOU_AUTH_ENABLED=1`とTLS証明書・鍵を指定する。rootの開発コマンドは既定値を準備する。公開URLを変更したらOry側のissuer、UI URL、return URLも合わせ、bootstrapでclientのredirect URIを更新する。

| 変数 | ローカルの既定値・用途 |
| --- | --- |
| `KOUJOU_SITE_URL` | `https://localhost:1420`。Webの公開origin |
| `KOUJOU_PUBLIC_API_URL` | `https://localhost:8080`。nativeが固定するAPI origin |
| `KOUJOU_OIDC_ISSUER` | `http://127.0.0.1:4444/`。HTTPを使う開発構成はloopbackに限定 |
| `KOUJOU_WEB_CLIENT_ID` / `KOUJOU_WEB_CLIENT_SECRET` | `koujou-web` / setupが生成。secretはGoだけに置く |
| `KOUJOU_NATIVE_CLIENT_ID` / `KOUJOU_NATIVE_REDIRECT_URI` | `koujou-native` / `http://127.0.0.1:19487/callback`。nativeにはsecretを使わない |
| `KOUJOU_API_AUDIENCE` | `koujou-api` |
| `KOUJOU_AUTH_KEY` | DBのOAuth資格情報・PKCE verifier用のAES鍵。setupが生成 |
| `KOUJOU_HYDRA_ADMIN_URL` | `http://127.0.0.1:4445`。Goからだけ使う |
| `KOUJOU_KRATOS_PUBLIC_URL` / `KOUJOU_KRATOS_ADMIN_URL` | `http://127.0.0.1:4433` / `http://127.0.0.1:4434` |
| `KOUJOU_TLS_CERT` / `KOUJOU_TLS_KEY` | root開発コマンドでは`.cache/tls/cert.pem` / `key.pem` |
| `KOUJOU_DEV_CA_FILE` | native開発時のloopback API用CA。rootが自身の開発証明書を渡す |

ViteのproxyはAPI認証ルートだけをGoへ送り、`/auth/login`等の画面はSPAで処理する。標準設定はローカル開発用で、外部公開用の配備手順を提供するものではない。外部公開時はKratosのSMTPを実配送先へ変更し、DB・管理API・Mailpitを公開しない。

`.env`の鍵はDB backupと別に保全する。暗号鍵を失うと保存済みBFF資格情報は復号できず再ログインが必要になる。DBの通常のbackup手順は[保存と復旧](persistence.md)を参照する。パスワード・token・確認コード・QRの秘密値をIssueやログへ貼らない。

## 検証

`pnpm run test:auth`は独立したCompose project、DB volume、loopback portを確保し、実Kratos・Hydra・Mailpitを起動する。試験環境のOryを個別に停止・再開し、終了時は所有labelを確認してそのprojectだけを削除する。標準開発環境のDBやログインは変更しない。

Windowsの実ブラウザ試験はAPI・Oryの起動後、明示した一時保存先で実行する。現在のブラウザで検証用アカウントへログインしておく。自身のOAuth認証・更新・失効を行うため、通常のCIではignoreする。

```powershell
$env:KOUJOU_DEV_CA_FILE = Join-Path $PWD '.cache/tls/cert.pem'
$env:KOUJOU_NATIVE_TEST_FILE = Join-Path $PWD '.cache/native-auth-test.dpapi'
cargo test -p koujou-san-desktop live_ory_browser_restart_refresh_logout -- --ignored --nocapture
```

通常の`check / test / build`と`check:desktop / test:desktop`、CIの実Ory試験も変更範囲に応じて実行する。認証不能と未ログインを分け、Ory停止時にログアウト済み・revoke済みと断定しない。
