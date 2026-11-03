# 0038: UIからのWorkspace認証と接続割当

状態: 採用

## 判断

通常のWorkspace準備と開発はkoujou-sanで完結させ、別途AIエージェントによる設定代行を前提にしない。Giteaは既存アカウントのOAuthログインをkoujou-sanから開始する方式を本人が選択した。初回のOAuthアプリ登録だけはGitea側で行う。専用Botは作らない。

サーバー管理者がGitea instance・client ID・secret・必要な追加CAをUIで登録する。Workspace管理者は登録済みアプリでログインし、Giteaのアカウントを確認してから、そのWorkspaceの共有接続として保存する。認証完了だけでは共有やrepo作成を行わない。作成許可・所有者・公開範囲は別に指定する。既存PAT接続とファイル設定は維持する。

認可要求はstate、PKCE S256、開始者、Workspace、10分の期限、OAuthアプリ版に結び付ける。公開callbackは一度だけcodeを交換し、現在の管理権限を確認して暗号化した候補を保存する。callbackを開くブラウザとアプリが異なるDesktopにも対応するため、callback自体にアプリのCookieを要求しない。共有保存は開始した本人の通常認証と現在のWorkspace管理権限を必須とし、候補のアカウントを画面で確認する。

アプリsecretと接続tokenは用途・Workspace・IDに結び付くAES-GCMで暗号化し、応答と共有履歴へ返さない。Giteaへのtoken交換は登録済みoriginとCAを使用し、redirectへsecretを転送しない。期限切れtokenはDBの接続行をロックしてrefreshし、一組の回転結果を保存する。再認証は接続IDを維持し、共有設定の版で競合を検出する。repo取得、観測、起票も同じ接続解決を使う。

認証済みサーバーLLMの登録・本人認証は既存の運用方式を維持する。サーバー管理者はUIで対象Workspaceへ接続を割り当て、取り消せる。DBの明示した許可・取消を既存ファイルのWorkspace設定より優先し、未設定時は従来の設定を使う。Workspace管理者によるサーバー資格情報の取得や、新規Workspaceへの自動割当は行わない。クライアント操作、Hopper、Triageは共通Gatewayで現在の割当を確認する。

## 影響と確認

migration 027はOAuthアプリ・期限付き認証候補・LLM割当を追加する。既存の版履歴、PAT暗号文、Context、Runを変更しない。追加データもDBと元のサーバー認証キーを使ってbackup・復元する。

[Gitea公式OAuth仕様](https://docs.gitea.com/development/oauth2-provider/)と[28.2.0のprovider実装](https://github.com/go-gitea/gitea/blob/v28.2.0/routers/web/auth/oauth2_provider.go)を確認した。所有HTTPと専用DBでstate・PKCE・権限・期限・回転・secret除外を検証し、実フォームからLLM割当、Gitea認証、Project・非公開repo・相談スレッドを準備する。実アカウントの確認と画面の実表示は別の証跡として扱う。受入条件は[作業記録](../progress/2026-11-03-ui-workspace-setup.md)を参照する。
