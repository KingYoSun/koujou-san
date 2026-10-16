# GitHubの観測接続

GitHub上のIssue・PR・commit・Actionsを配置したまま観測する。PR作成・merge・CI起動はこの接続に含めない。判断は[ADR 0012](../adr/0012-github-observation.md)、現在の確認結果は[進捗記録](../progress/current-state.md)を参照する。

## 接続設定

GitHub Appを利用する。必要なrepository permissionsはActions・Contents・Issues・Pull requests・MetadataのRead-only。組織権限やユーザーのOAuth認証は要求しない。Appを対象repositoryだけへインストールし、App ID・installation ID・repository IDを確認する。秘密鍵を生成し、サーバー実行者だけが読めるGit管理外のファイルに保存する。実際の鍵やtokenを文書・チャット・ログへ記載しない。

`KOUJOU_GITHUB_CONFIG`にJSON設定の絶対パスを指定する。次の例のID・鍵パス・Workspace IDは実際の値へ置き換える。`bindings`は共有先を表す。同じ外部対象は一つのtargetへまとめ、複数のbindingsから参照する。

```json
{
  "app_id": "APP_ID",
  "private_key_file": "C:/private/koujou-github.pem",
  "interval_seconds": 60,
  "targets": [
    {
      "id": "kukuri-ci",
      "installation_id": 1,
      "repository_id": 1025894008,
      "repository": "kukuri-app/kukuri",
      "kind": "actions",
      "external_id": "38784153986"
    }
  ],
  "bindings": [
    {
      "workspace_id": "WORKSPACE_ID",
      "target_id": "kukuri-ci",
      "expected_commit": "ec3a86cf6a02bfd97079fd7a2e918afd06096c3c",
      "required_jobs": []
    }
  ]
}
```

targetの`kind`は`issue / pr / commit / actions`。`external_id`はIssue/PR番号、40桁のcommit SHA、Actions run IDのいずれかを指定する。Actionsの`attempt`を省略すると現在の回を照合し、正の整数を指定するとその回を固定する。target IDと外部対象の対応は変更せず、別対象には新しいIDを使う。設定の変更後はAPIサーバーを再起動する。

既存Workspaceをbindingsへ指定する。共有先への参加と現在のresource権限をAPIで確認する。resource IDは`github:<target ID>`であり、Scope所属や他の参照から権限を付与しない。設定を外しても観測履歴は削除しない。

## Webhook

公開HTTPS入口がある場合は`POST /github/webhook`をAppへ設定する。ローカル利用で公開入口がない場合はAppのWebhookを無効にし、定期照合を使う。

Webhookを有効にするときは、GitHubに設定したsecretと同じ内容のファイルを`webhook_secret_file`で参照する。ファイルの改行もsecretの一部になる。TLS検証を有効にし、Issues・Pull request・Push・Workflow run・Workflow jobの必要なイベントを選ぶ。サーバーは署名を検証し、登録済みinstallation/repositoryに一致する対象だけを再取得待ちにする。重複や逆順のpayloadで結果を書き換えない。

## 表示と履歴

共通UIでWorkspaceを選び「GitHubの共有観測」を開く。指定commitとの一致、workflow・run・実行回、CIの結論、必要ジョブと全ジョブ、取得元の更新時刻、最終確認と最終成功取得時刻を確認できる。必要ジョブは設定した照合対象であり、GitHubの保護規則から自動推測したものではない。

「共有観測を再読込」はサーバーの記録を取得する操作で、CIを開始しない。「GitHubで開く」は外部の正本へ移動する。履歴と対象はページで表示する。Unknownや定期照合の遅延がある場合、前回の成功結果だけで現在の合格を判断しない。

## 検証コマンド

- `pnpm run test:github`: 専用PGと固定GitHub応答による保存・認可・通知・失敗・再起動。
- `pnpm run test:github:live`: 明示したApp設定で実GitHubを読む。秘密値は出力せず、対象ID・版・観測時刻を記録する。
- `pnpm run test:github:ui`: 実Ory・専用PG・実Appの共通UI検証。Appの権限は変えず、設定のbindingsだけを検証用Workspaceへ置き換える。標準環境のユーザーやデータは使わない。

live/UIは`KOUJOU_GITHUB_CONFIG`の設定が必要で、通常CIから実GitHubの資格情報を要求しない。UIの起動先と終了方法は試験ログに出力される。Windowsアプリの検証用OAuthメタデータは専用ファイルへ保存されるため、本文やtokenをチャットへ貼らない。画面操作は[Computer Use手順](computer-use.md)に従う。
