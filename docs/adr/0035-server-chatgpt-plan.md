# 0035: サーバーのChatGPTプラン接続

- 状態: Accepted。2026年11月2日の直接依頼と出力上限撤回の回答による。
- 範囲と判定: [実装計画](../progress/2026-11-02-server-chatgpt-plan.md)。

## 判断

サーバーLLM接続へ`type=chatgpt`を追加し、Sign in with ChatGPTのOSS向けプラン利用権限でResponses APIを直接使う。GoのHTTP clientと既存OIDC検証を用い、Codex CLI・SDK・個人の既存認証を流用しない。HopperとTriageは認可、入力確定、予約、応答保存、判定、利用量記録を共用する。

認証はサーバー運用者が管理コマンドで行う。新規登録には`dynamic_agent_client`とこのホストの安定したIDを使い、発行されたclient IDと検証済みsubjectを保存する。同じ接続の再認証では同一account登録を維持する。state・PKCE・nonce・署名・issuer・audienceとプラン利用scopeを検証する。モデル要求中に対話ログインを始めない。

資格情報は接続ごとのprivateなファイルへ原子的に保存する。WindowsではDACL、Unixではowner-only権限を適用する。同じファイルのrefreshはOSのファイルロックで直列化し、回転したrefresh tokenを一組として置き換える。外部へのredirectを追わない。account登録・hostが変わると接続revisionを変え、tokenの更新だけでは変えない。

推論先は公式の`https://api.openai.com/v1/responses`に固定し、Bearerへ専用のOAuth access tokenだけを付ける。`store=false`、`stream=true`を指定し、`response.completed`を完了として扱う。元の受信streamと送信用本文を保存し、実モデルとusageを共通判定形式へ読み取る。失敗・未完了・切断・時間切れを成功にしない。許可した実モデルとの照合と、現行権限による採用判定を維持する。

## 上限と利用量

公式previewが`max_output_tokens`を受け付けないため、本人判断によりChatGPT接続には出力token上限を設けない。定義へ`unlimited_output=true`と`limits.output_tokens=0`を明示する。従来の定義・HTTP Gatewayは正の出力上限を維持する。入力の制限は変換後の送信用本文のUTF-8サイズへ適用し、時間の上限も維持する。

利用量はproviderが報告した数量を保存し、サブスクだから0としたり、API料金を実請求へ読み替えたりしない。明示された料金版による既存の推定と、報告されたtoken数を分ける。ChatGPT接続に自動fallbackを追加しない。

## 保存と配布

標準配布の`config/connectors/chatgpt/`だけをAPIへ書込み可能にmountし、通常の設定mountはread-onlyのままとする。既存のconfig backupが認証・host登録・最新tokenを含む。本人が操作するOAuth callbackはHTTP loopbackに限定し、公開Webへ認証情報を入力させる経路やCLI実行機能を追加しない。

[ADR 0027](0027-server-hopper-semantics.md)の既存Gateway方式は維持する。本ADRは、サーバー接続をGatewayだけに限る前提と出力token上限について、ChatGPT接続の範囲を追加する。クライアントのモデルログインとRunのCLI認証の責務は変更しない。

## 公式仕様

[登録・認証](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)、[sessionの更新](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)、[推論](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)、[preview制約](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)に従う。実契約での提供モデルや同意は、本人の接続時の応答で確認する。
