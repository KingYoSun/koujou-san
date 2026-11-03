# サーバーのモデル接続とHopperの意味判断

クライアントのモデル接続と別に設定する。サーバーの意味判断にCLIや個人の既存認証ファイルを流用しない。HTTP GatewayとChatGPTプラン接続を選べる。契約は[ADR 0035](../adr/0035-server-chatgpt-plan.md)を参照する。

## ChatGPTプランへのログイン

サーバー専用のSign in with ChatGPT登録を作り、GoからResponses APIへ直接接続する。Codexは使わない。次のコマンドは本人認証用で、モデルへの推論要求は行わない。

開発checkoutでは、privateな保存先を絶対パスで指定する。

```powershell
pnpm run server-model:login --credentials-file C:/private/koujou-chatgpt/account.json
pnpm run server-model:models --credentials-file C:/private/koujou-chatgpt/account.json
pnpm run server-model:logout --credentials-file C:/private/koujou-chatgpt/account.json
```

標準Linux配布では同じ操作を`./koujou model-login`、`model-models`、`model-logout`で行う。例の保存先は導入先に置き換える。

```sh
./koujou model-login --credentials-file /private/deployment/config/connectors/chatgpt/account.json
./koujou model-models --credentials-file /private/deployment/config/connectors/chatgpt/account.json
```

表示された「Continue with ChatGPT」のURLを本人のブラウザで開き、accountとworkspaceを選び、プラン利用へ同意する。callbackは`http://127.0.0.1:14557/auth/callback`。別のportを使う場合は`--callback-port`で指定する。新規登録には安定したhost IDを保存し、発行client ID・ID tokenの署名・issuer・audience・nonce・subject・利用scopeを検証してから認証情報を保存する。

リモートサーバー上の認証コマンドへ本人のブラウザから戻す場合は、事前に本人の端末からloopback portを転送する。下の`USER@SERVER`は実際の接続先に置き換える。

```sh
ssh -L 14557:127.0.0.1:14557 USER@SERVER
```

このSSH sessionでサーバーの`model-login`を実行し、表示URLを本人の端末で開く。公開HTTPS callbackへ置き換えない。別のaccountを追加するときは別のcredentialファイルを使う。同じファイルへの再認証で別のsubjectへ置き換えない。別ホストへ移設するときは新しいcredentialファイルでホスト専用の登録を作る。

## ChatGPT接続の登録

認証済み接続を初回登録した後、サーバー管理者は「設定→サーバーLLM接続→Workspaceへの割当」で、現在のWorkspaceへの許可・取消を保存できる。通常のWorkspace追加では設定ファイルを書き換えない。明示した割当・取消はファイルの`workspaces`より優先し、UIで未設定の組は既存設定を維持する。Workspace管理者だけではサーバー接続を割り当てられない。資格情報の登録と本人ログインは下記の初回運用手順で行う。

登録済みの接続は、Webの「設定 → クライアントLLM接続」からも取得・選択できる。Projectチャット、Context選択等のモデル要求をサーバーが中継し、資格情報はブラウザへ返さない。使い方は[クライアント接続](client-context-selection.md)、契約は[ADR 0036](../adr/0036-web-server-models.md)を参照する。登録・ログインは下記の運用者設定を用いる。

`KOUJOU_SERVER_LLM_CONFIG`の接続一覧へ追加する。要求モデル名は`model-models`が返す現在のaccountのslugから選び、許可する実モデル名も明示する。モデル名をSDKの例だけで決めない。

```json
[
  {
    "type": "chatgpt",
    "id": "server-chatgpt",
    "name": "サーバーのChatGPTプラン",
    "credential_file": "/config/connectors/chatgpt/account.json",
    "workspaces": ["WORKSPACE_ID"],
    "models": [
      { "requested": "MODEL_SLUG", "allowed_actual": ["ACTUAL_MODEL"] }
    ]
  }
]
```

標準配布ではhost側の`config/connectors/chatgpt/`が、上のcontainer内pathへ書込み可能にmountされる。その他の実行方式ではAPI実行者だけが読み書きできる保存先を指定する。OAuth tokenの更新・回転はAPIがファイルロックで直列化し、成功した一組を原子的に保存する。同じcredentialを複製して複数のruntimeからrefreshしない。資格情報と認証URLをチャット・Issue・Context・共有ログへ貼らない。

接続のrevisionはaccount登録とhostに結び付く。token更新では変わらず、別登録へ切り替えた場合はHopper・Triageの定義で接続を選び直す。サーバーのモデル一覧から接続を選ぶと、出力上限は「上限なし」になる。保存する定義は`unlimited_output=true`、`limits.output_tokens=0`とし、入力byteと時間の上限は従来どおり指定する。API接続の出力上限は維持する。

この接続は`store=false`・`stream=true`で推論し、terminalの`response.completed`まで成功にしない。原stream、送信用本文、実モデルと報告usageを保存する。未算定の費用やサブスク利用量を0へ置き換えない。fallbackは空で使う。ChatGPTの利用権限や上限に達した際は失敗・保留として確認し、APIキーへ自動で切り替えない。

認証が失効した場合は、本人が`model-login`を再実行する。Workerから対話ログインは始まらない。`model-logout`はrefresh tokenの失効を試し、確認できたかを表示したうえでローカルのtokenを削除する。失効未確認の場合はChatGPT設定から接続も確認する。登録とhost IDは再ログイン用に保持する。既に送信済みの推論が取り消されたとは扱わない。

標準backupは`config/`内のcredentialも含む。APIの書込みを停止した一貫したbackupと、対応するCA・秘密値をprivateな保存先で保全する。実accountでの認証・推論の成功は、所有HTTP/JWKS/SSE fixtureの成功と区別する。[公式の登録手順](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)と[preview制約](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)も参照する。

## 任意Gatewayの起動

```powershell
pnpm run infra:llm:up
pnpm run infra:llm:stop
```

`infra:up`ではBifrostを起動しない。`infra:llm:up`は不足するローカル認証と設定を準備し、`llm` profileのBifrostだけを起動する。管理画面は`http://127.0.0.1:18080`。管理名は`koujou-admin`、秘密値はGit管理外の`.env`にある`BIFROST_ADMIN_PASSWORD`を本人の端末で確認する。秘密値をチャット・Issue・ログへ貼らない。既存の設定・認証・volumeは通常停止で保持する。

固定版は`maximhq/bifrost:v2.3.2`、digest `sha256:f5b18826349fe9e1c402c10d19f1cbb5567a3c800ad928580d07f72e69106ec7`。sourceは`transports/v2.3.2`の`a05ea20e68bf116a35e9f05b1a3d749624b3935e`。Apache-2.0の[原ライセンス](../../infra/bifrost/LICENSE)を保持する。実行依存は同imageのLinux・Go HTTP binary・SQLiteで、専用PostgreSQLは不要。サーバーは既存のGo HTTP clientで接続し、新しいSDK依存を追加しない。

## 接続を登録する

初期のproviderと接続一覧は空で、モデル要求先は未登録である。運用者が次の二つを設定する。

1. `.cache/bifrost/config.json`の`providers`へ、明示したprovider・モデル・鍵を登録する。鍵は`.cache/bifrost/provider.env`へ置き、設定の`value`から`env.BIFROST_PROVIDER_KEY`のように参照できる。providerごとの`network_config.max_retries`を`0`にし、`default_request_timeout_in_seconds`も設定する。Worker用virtual keyの`provider_configs`へprovider、`key_ids`、`allowed_models`を明記する。
2. `.cache/bifrost/connections.json`へ、APIが使う接続を登録する。次は値を置換する雛形であり、そのまま実接続する設定ではない。

```json
[
  {
    "id": "server-primary",
    "name": "サーバーのモデル接続",
    "endpoint": "http://127.0.0.1:18080/v1",
    "key_file": "C:/Users/USER/koujou-san/.cache/bifrost/worker-key",
    "workspaces": ["WORKSPACE_ID"],
    "models": [
      {
        "requested": "openai/PROVIDER_MODEL",
        "allowed_actual": ["ACTUAL_MODEL"]
      }
    ]
  }
]
```

`key_file`はWorker用virtual keyのファイルで、上流providerのキーとは異なる。`ca_file`を指定すればそのCAを追加する。ファイルパスは絶対パスを使う。`workspaces`は許可するWorkspace IDを指定し、Contextごとの許可一覧を作らない。本文の送信禁止分類はWorkspaceの共有ポリシーで管理する。`models`は要求名と実応答名を対応させる。fallback先も登録し、Hopper定義で使うものだけを明示する。

API側の`endpoint`と、Bifrostが上流へ接続する`network_config.base_url`は別の設定である。OpenAI providerの`base_url`は省略して既定値を使う。Bifrost側が`/v1/chat/completions`を付けるため、上流設定へ`/v1`を重ねない。

APIは`KOUJOU_SERVER_LLM_CONFIG`のJSONを要求ごとに読む。`dev:api`はroot基準の設定パスを解決する。それ以外の起動では絶対パスを指定する。接続内容が変わると公開revisionが変わるため、Hopper定義で接続を選び直し、新しい版を保存・有効化する。APIのセッションやMCP tokenをGatewayへ転送しない。Gatewayへのredirectも追跡しない。

Gateway設定JSONは`source_of_truth=config.json`である。管理画面だけで変更した設定を、永続する設定として扱わない。設定・provider環境変数の変更後は、`infra:llm:up`でコンテナを再作成する。providerの認証、モデルへの要求と料金は、利用者が明示した接続だけで確認する。

実接続の受入では、本人が接続・認証・消費を許可した後に限り、`pnpm run test:hopper:live 接続設定JSONのパス`を使う。設定は一つの接続・一つの要求モデルとし、試験用Workspace `one`を明示する。この入口は専用PGと所有Node出力だけを使い、要求は一回・入力4 KiB・出力2048 token・30秒、fallbackなし。実LLMへ障害fixtureや後続CLIの実行を送らない。失敗時も再要求せず停止する。二度目の実行は残る許可枠を確認する。消費と判定のメタデータは`test-results/issue27-live.json`へ保存する。

## 本文ログと外部通信

Triageも同じ接続登録と要求・応答・利用量の保存処理を使う。方針・指示・Context・上限・処理枠はHopperとは別に設定する。操作と実接続用の有界な入口は[Triageの手順](automation.md)を参照する。

共有雛形では本文ログ・object storageへの本文保存・要求単位の本文保存上書き、semantic cache、Maxim、OTEL、Warpを無効にする。Gatewayの稼働・要求状態のメタデータは専用SQLiteへ保存する。Go側の固定入力・原応答・費用履歴は別に保存し、Gatewayのログを判定の正本にしない。

上流への通信先は、運用者が登録したproviderと明示したfallbackである。加えてBifrostの起動時のモデル・料金カタログ取得がある。定期のlive model・MCP library同期は無効とする。外部送信先を固定する運用では、`framework.pricing.pricing_url`と`model_parameters_url`を所有するミラーへ向け、providerとそのミラーだけをネットワークで許可する。隔離probeでは所有HTTPと内部ネットワークで検証した。標準Compose自体にホスト全体の通信制限を設定するものではない。

## Hopper画面

「Hopper」の定義を開き、「サーバーのモデル接続を取得」から接続を選ぶ。通常条件に意味判断を追加し、指示・入力/出力/時間の上限を設定する。評価Context、必要な証拠、許可fallback、料金版と費用上限は定義JSONに保存する。必要な証拠はContext等のIDで指定し、版なしは到着・変更を追う。固定版ならその版だけを読む。

保存、有効化、停止、提案の発行は別操作である。過去の結果は対象・ページを指定して発行せず評価できる。意味判断はこの試行でもモデル消費が発生する。受付後は「評価と発行」を更新し、成立理由、引用、入力待ち・判断不能・失敗、試行・消費、元結果と発行先を確認する。原入力・原応答は固定版を明示して取得し、一覧で全本文を取得しない。

失敗や保留から再評価する場合は「同じ結果を新しい履歴で再評価」を使う。追加の消費を記録し、前の評価と発行先を保持する。連鎖が停止・上限到達なら、上限と停止理由を確認し、連鎖の明示再開を先に行う。Gateway停止中も決定論Hopperと手動操作は利用できる。

## バックアップと更新・復旧

SQLiteの整合したコピーを取るため、`infra:llm:stop`でGatewayだけを停止する。専用volume `koujou-san-dev_bifrost-data`、`.cache/bifrost/config.json`、`provider.env`、`connections.json`、`worker-key`、`.env`のBifrost用認証と暗号鍵を一組として保全する。その他のサービスの認証を含めて共有しない。コピーはGit管理外・利用者だけが読める場所へ置く。

volumeは固定imageの`tar`を使い、読取り専用でマウントして取得できる。復旧は新しい専用volumeへ展開し、復旧先の所有者をimageのUID/GID `1000:0`へ合わせる。復元前後の`config.db`・`logs.db`のSHA-256を比較してから、同じ暗号鍵と設定で固定版を起動し、管理ログイン・provider設定・本文ログ無効を確認する。新しい空の暗号鍵で既存DBを開こうとしない。

更新では、先に一組のバックアップを作る。新しい版とdigest、ライセンス、設定差分、構造化応答・usage・fallback・本文ログの挙動を所有する合成入力で確認してからimageを変更する。旧版へ戻す際は、旧imageと更新前のvolume・設定・鍵を組で復元する。migration済みDBへ旧binaryだけを戻さない。実providerへの追加要求は、接続と消費枠を明示して確認する。
