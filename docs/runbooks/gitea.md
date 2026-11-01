# self-hosted Giteaへの接続

観測・起票はサーバー側の既存サービス、PR等の操作は既存RunのCLI/APIを使う。[ADR 0034](../adr/0034-gitea-integration.md)と[受入記録](../progress/2026-11-01-issue-83-evidence.md)を参照する。検証対象はGitea **28.2.0**、Gitea Runner **5.2.0**。他の版は未確認である。

## 観測の設定

一般ユーザーの対象repoへのアクセスと、`read:repository`・`read:issue`を持つtokenを用意する。token、CA、Webhook secretはサーバー実行者だけが読めるGit管理外のファイルへ保存する。秘密値をJSONの値、Context、Run入力、ログへ書かない。

`KOUJOU_GITEA_CONFIG`に次のJSONの絶対パスを指定する。URL、ID、repo、Workspace、各ファイルのパスは実際の値へ置き換える。GitHubも使う場合は既存の`KOUJOU_GITHUB_CONFIG`を併用する。GiteaだけならGitHub Appの鍵は不要である。

```json
{
  "interval_seconds": 60,
  "gitea_instances": [
    {
      "url": "https://gitea.example.lan",
      "token_file": "C:/private/gitea-read-token",
      "ca_file": "C:/private/gitea-ca.pem",
      "webhook_secret_file": "C:/private/gitea-hook-secret"
    }
  ],
  "targets": [
    {
      "id": "gitea:project-ci",
      "provider": "gitea",
      "instance": "https://gitea.example.lan",
      "repository": "team/private",
      "repository_id": 1,
      "kind": "actions",
      "workflow": "proof.yml",
      "event": "pull_request"
    }
  ],
  "bindings": [
    {
      "workspace_id": "WORKSPACE_ID",
      "target_id": "gitea:project-ci",
      "required_jobs": ["Linux", "Windows"]
    }
  ]
}
```

`kind`は`issue / pr / commit / actions / status`。`external_id`にはIssue/PR番号、40桁のcommit、Actions run IDを指定する。statusはcommitを指定する。固定Actions runは任意の`attempt`で実行回も固定できる。新しい周期を見る場合は上記のように`workflow`へファイル名を指定し、`external_id`・`attempt`は指定しない。数値workflow IDはGitHub用であり、Giteaでは使わない。

Gitea target IDは`gitea:`で始め、接尾辞は英数字・`_`・`-`を使う。認可資源もこのIDである。同じIDの外部対象を変更せず、別のinstance・repo・対象には新しいIDを付ける。既存GitHubの資源ID・保存参照は維持する。`bindings`は共有先であり、Scope所属から閲覧権限を得るものではない。設定変更後はAPIを再起動する。保存・読込みだけでは外部書込みやRun開始を行わない。

設定したLAN接続先を扱える。HTTPSでは通常の証明書検証に`ca_file`のCAを追加し、検証を無効にしない。redirectへtokenを転送しない。初回にrepoの数値IDと`full_name`、各対象の取得、返されたサーバー版を照合し、必要なAPIとtoken権限を確認する。

## 通知と定期照合

各instanceのURL文字列を正規化し、そのSHA-256（小文字hex）を`POST /gitea/webhook/{hash}`の宛先にする。Giteaのrepo Webhookへ同じsecretを設定する。ファイルの改行もsecretの一部となる。Issue・PR・push等、必要な通知を選び、Giteaから到達できる入口を使う。

署名`X-Gitea-Signature`とrepo IDを検証し、そのinstanceの許可対象だけを再取得待ちにする。payloadの本文や成功表示で共有状態を確定しない。通知の重複・逆順・欠落は定期照合で収束させる。接続失敗時は最終成功取得と現在の未観測を分け、`Retry-After`中は再要求を保留する。

共通UIの「GitHub・Gitea観測」でinstance、repo、サーバー版、commit、run、実行回、必要job、取得時刻、外部出典を確認する。再読込はCI開始操作ではない。`/forge/workspaces/{workspace}/observations`は両provider、従来の`/github/…`一覧はGitHubだけを返す。

## 起票と優先度

`KOUJOU_PUBLICATION_CONFIG`の`gitea_instances`には起票用tokenファイルを指定する。必要なscopeは`write:issue`と`read:repository`。接続ごとに`provider / instance / repository / repository_id`を固定する。

```json
{
  "gitea_instances": [
    {
      "url": "https://gitea.example.lan",
      "token_file": "C:/private/gitea-issue-token",
      "ca_file": "C:/private/gitea-ca.pem"
    }
  ],
  "connections": [
    {
      "id": "project-issues",
      "workspace_id": "WORKSPACE_ID",
      "provider": "gitea",
      "instance": "https://gitea.example.lan",
      "repository": "team/private",
      "repository_id": 1,
      "actor_id": 1
    }
  ]
}
```

`actor_id`は専用自動化ユーザーの数値IDで、ラベル操作の自分/外部の区別に使う。人の操作には別のユーザーを使う。Triageの接続IDと既存ラベル名の対応は[優先度同期](automation.md)と同じで、Issue情報の`origin`は`gitea`を使う。

[起票手順](issue-publication.md)の固定下書き・プレビュー・共有受付・一回の送信意図・marker照合を共用する。作成の応答喪失や検索反映待ちでは、未作成と断定して再送しない。同じ要求のまま既存Issueを照合する。

## PR・CI・検品・マージ

PR作成、CI再実行、検品status、mergeは、固定入力を持つ[Run](runs.md)で明示したCLI/APIへ依頼する。操作用tokenはその端末の接続設定で管理し、モデル入力へ渡さない。実行前後にinstance・repo ID・対象commitと外部結果を取得する。PR作成でも送信意図を保存し、結果不明時に自動再作成しない。

Workflowの`ci.target`とHopperの`kind=gitea`には登録したtarget IDを指定する。CIの結論は終了コード0と別に判定する。Workflowではcommit、run ID、必要jobを固定する。未観測・skipped・cancelledをPASSにしない。最新workflowの別commitが返った場合もPASSにしない。

独立検品は固定成果物と条件を別セッションへ渡し、実装会話・自己評価を自動注入しない。現行の検品PASSに対応するcommitへstatusを公開し、Giteaのmain保護へ必須CIとそのstatus名を設定する。mergeは現在のPR headを再取得し、`head_commit_id`を条件にして`force_merge=false`で送る。保護規則と現在の許可を迂回しない。

## 二種類のRunnerとLFS

Gitea RunnerはGiteaのjob用tokenで非公開repoをcheckoutし、LFSを取得してbuild/testとartifact保存を行う。LinuxとWindowsへそれぞれ登録する。koujou-sanのネイティブRunnerは採用したRunのCLIを端末で実行する。登録tokenと資格情報、担当するjobを混同しない。

Windowsのhost jobではRunner自身のPATHでもGit Bashを選べるようにする。Git Schannelを使い明示CAを渡す場合は`http.schannelUseSSLCAInfo=true`も必要となる。Runnerの配布物・Actions・artifact通信のCAも、それぞれのプロセスへ指定する。公開Actionsを使う場合は公開CAを残し、私設CAだけで上書きしない。

LFSの本体やbuildバイナリはContext本文にコピーしない。repo・commit・LFS OID・artifact ID/URI・digestを記録し、必要な本文だけを固定入力へ選ぶ。

## 検証入口

`pnpm run test:forge`はGitHubとGiteaの専用DB契約試験、`test:publications`は起票の専用DB試験。`KOUJOU_GITEA_CONFIG`と`KOUJOU_PUBLICATION_CONFIG`を明示した`pnpm run test:gitea:live 証跡ディレクトリ`は、許可済みの専用repoで観測・一回の起票・優先度ラベルの取得/反映を確認する。`pnpm run test:gitea:ui`は実Oryと専用DBの共通UIを起動する。通常CIは実資格情報を要求しない。実接続の操作と後片付けは、合意した専用環境の範囲で行う。
