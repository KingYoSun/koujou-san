# 0034: Giteaの外部識別と既存工程への接続

- 状態: Accepted。実装・実接続の受入判定は[計画](../progress/2026-11-01-issue-83-plan.md)で管理する。
- 日付: 2026-11-01
- 根拠: Issue #83、ADR 0012・0022、既存Run / Workflow / Hopper / Triageの契約。

## 外部識別と互換性

Giteaの観測対象はprovider、正規化したインスタンスURL、repositoryとそのID、対象種別と外部IDを保持する。Gitea target IDは`gitea:`で始め、既存のGitHub target IDと区別する。対象の割当てを変更せず、同じIDで異なる外部対象へ接続し直すことを拒否する。

既存GitHubのtarget、保存済みJSON、`github:<target ID>`の認可資源と参照は維持する。Giteaの認可資源はその`gitea:`付きIDとする。資源IDを得る処理を共用し、観測・工程・外部イベントの利用側を接続する。既存の保存・版・ページ取得・Workerを使うため、従来の内部package名とtable名の一括改名は行わない。

## 接続と送信

Gitea観測は`KOUJOU_GITEA_CONFIG`、起票は既存の`KOUJOU_PUBLICATION_CONFIG`を使う。インスタンスごとに明示したURL、tokenファイル、必要なCAを固定する。観測と起票は別のtokenを指定できる。秘密値はサーバー側のGit管理外ファイルから読み、Target・Snapshot・共有Run・Contextへ複製しない。別originへのredirectへ認証を転送しない。

LAN内の明示した接続先を許可し、HTTPSの検証を有効にしたまま指定CAを追加する。接続設定の読込みだけでは外部書込みやRun開始を行わない。既存GitHub Appの認証・権限要求は変更しない。

## 観測と実行

Gitea用のHTTP処理は観測と起票で共用し、API応答を既存の状態記録へ変換する。新しい観測WorkerやRun実行エンジンは作らない。Issue / PR / commit / Actions / statusを扱い、GitHubにない値を合成して同じに見せない。

28.2.0の公式APIでは、Actionsのworkflow指定はファイル名であり、runの数値workflow IDは返らない。Giteaではファイル名を保持し、GitHubの数値IDは従来どおり扱う。job一覧は`limit`と`page`、run / jobの`run_attempt`を照合する。取得途中の変更、欠けたページ、接続断は完全な成功として保存しない。Webhookはインスタンスごとに署名とrepoを照合し、payloadを正本として採用せず再取得を促す。

起票は既存の一回の送信意図と照合markerを使う。結果不明では自動再作成せず、該当repoの候補をページ取得して照合する。PR作成、CI再実行、検品status、マージは既存RunのCLI / API操作を使い、固定した入力と外部で確定した結果を再取得する。GiteaのマージAPIの`head_commit_id`とブランチ保護を使い、新headに古いPASSを流用しない。

## 検証する版と根拠

最初の検証対象はGitea 28.2.0、Gitea Runner 5.2.0。配布物の実版と公開SHA256を確認する。対応版は実体で受け入れた範囲だけを手順へ記す。GitHub ActionsとのYAML類似性をAPI互換の根拠にしない。

公式の[API](https://docs.gitea.com/api/)、[28系OpenAPI](https://docs.gitea.com/openapi3-28.json)、[28.2.0のリリース](https://blog.gitea.com/release-of-28.2.0/)、[Runner配布](https://dl.gitea.com/gitea-runner/5.2.0/)、[ブランチ保護](https://docs.gitea.com/usage/access-control/protected-branches/)、[Webhook](https://docs.gitea.com/usage/repository/webhooks/)、[LFS](https://docs.gitea.com/administration/git-lfs-setup/)を2026-11-01に確認した。実体の観測・Linux / Windows Runner・共同一巡と失敗試行は、計画から辿る証跡へ残す。
