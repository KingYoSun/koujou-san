# 0036: Webのクライアント操作でサーバーLLM接続を使う

- 状態: Accepted。2026年11月3日の直接依頼による。
- 範囲と判定: [実装記録](../progress/2026-11-03-web-server-models.md)。

## 判断

WebのクライアントLLM接続へ、現在のWorkspaceに許可されたサーバー接続と登録モデルを追加する。既存の直結HTTPとデスクトップ接続を維持する。サーバー接続の選択時はブラウザへ認証情報を渡さず、サーバーの登録済み資格情報を用いる。接続名・モデル・接続版は登録内容から取得し、料金等の利用設定だけを利用者・API・Workspaceに結び付けて保存する。

対話と読取りtoolの実行は既存クライアントHostが担当し、モデルへの一回の送信をGo APIが中継する。Projectチャット、Context選択、定義生成、出力からのContext生成は同じ`operationPort`を使う。クライアント不在時の肩代わりや、WebからのCLI実行は追加しない。

## 認可と保存

送信前に、現在の本人・担当クライアント・固定turn・入力digest・接続revision・要求/実モデル許可・現在のポリシーと送信期限を照合する。送信先や上流の認証ヘッダはブラウザから受け付けない。既存のScope、除外、送信禁止分類と固定参照の検査を用いる。Ory/MCP cookie・tokenをモデルへ転送しない。

既存のPostgreSQLの版付き記録へ、turnごとの送信予約を確定してから外部通信する。一つのturnの送信は最大一回とし、未観測の予約を再送しない。原要求、送信したwire本文、原応答とterminal状態を固定記録へ保存する。切断や権限取消があっても外部応答の保存を試み、採用は現在権限で別に検査する。

サーバー接続の結果確定はサーバー内部からのみ許可し、ブラウザが作った結果を採用しない。クライアントは確定済みの操作を再取得して、既存のtool往復・質問・提案・採用へ続く。追加の原要求/原応答は一覧から除去し、固定版を指定した取得へ残す。利用量は操作に帰属させ、RunやHopper/Triageへ重ねない。実protocolと報告usage・原記録の由来を保存し、欠測やサブスク費用を0にしない。

## Responsesとの対応

ChatGPT接続は既存の共通Responses経路を拡張する。assistantの履歴、`koujou`名前空間のfunction tool、call/output、継続に必要な推論itemを扱う。MCPの引数schemaを変えないため、function toolは`strict=false`とする。構造化出力のschema指定は従来どおり用いる。

`store=false`、`stream=true`、HTTPでの全履歴送信とterminal判定を維持する。tool往復では`reasoning.encrypted_content`を要求し、受信した元のoutput itemを次の要求へ保持する。toolの実行は既存のMCP読取り許可に従い、providerの出力から権限を増やさない。

[ADR 0026](0026-client-context-selection.md)のうちモデル送信をクライアントに限る前提へ、上記の中継を追加する。[ADR 0035](0035-server-chatgpt-plan.md)の専用認証、秘密値の保存と出力上限なしの判断は維持する。Webからのサブスク登録・ログインは追加しない。

公式の根拠は[function calling](https://developers.openai.com/api/docs/guides/function-calling)、[Responses](https://developers.openai.com/api/reference/resources/responses/methods/create)、[SIWCの制約](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)である。
