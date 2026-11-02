# Webのクライアント操作でサーバーLLM接続を使う

## 目的と範囲

本人の依頼は、WebのクライアントLLMからサーバー設定済みの接続を使うことである。基準は`6a2fe388f8ad66cac9c81adf5284d1349e975f73`、開始時のcheckoutはclean。区分Cとして、認可・秘密値・外部送信・共有保存の経路を独立検品する。

対象は、Webの接続選択と、その共通Hostを使うProjectチャット、Context選択、定義生成、出力からのContext生成である。ブラウザが対話とMCPの読取りtoolを進め、予約済みの一要求だけをGo APIから登録済みモデルへ送る。サーバー接続の再登録やWebログイン、クライアント不在時の対話継続、Run/CLI実行、新しいproviderは追加しない。既存の直結HTTP・デスクトップ接続・Hopper/Triageは維持する。

| 条件 | 対象・期待結果 | 判定方法 |
| --- | --- | --- |
| AC-1 | WebでWorkspaceに許可されたサーバー接続と要求モデルを選択でき、キー入力なしでクライアント操作へ使える。既存の直結接続も選べる | 接続設定と共通Hostの自動UI試験。候補順、選択、Workspace切替、接続なし・取得失敗を照合 |
| AC-2 | モデル送信は現在の本人・操作担当・固定turn・入力digest・接続revision・要求/実モデル許可・現行ポリシーと送信期限に結び付く。任意URLへのproxyにしない | 所有HTTPと専用DBで他人/他Workspace、権限・接続変更、未予約/改変要求を拒否し、上流呼出し0回を確認 |
| AC-3 | 予約済みturnは最大1回送信し、二重要求・応答喪失・画面再読込で再送しない。上流の原要求/原応答はサーバーで保存し、ブラウザ提供の偽応答を採用しない | 並列/再実行/中断試験と保存記録を照合。開始後不明は明示的な結果不明として保持 |
| AC-4 | 登録済みChatGPT接続で通常対話、既存の読取りtool往復、構造化出力を扱い、実モデル・usage・原streamと推論履歴を保持する | 所有Responses/SSE fixtureでassistant/関数call/output/推論itemの再送とterminal状態を照合。既存HTTPとHopper/Triageも検査 |
| AC-5 | ProjectチャットのAI応答とContext操作の結果が既存の固定版保存・採用・利用量へ接続される。失敗・未完了・モデル不許可を成功にせず、一覧へ追加の本文を混入させない | 共通Hostと業務DBの正常/失敗試験、改変finish拒否、一覧と固定read、利用量のprotocol/数量/由来を照合 |
| AC-6 | 一般化した手順・設計判断と検証記録を残し、最終差分の独立検品と実際のPR CIを確認する | 対象試験・型・build・整形、固定commitの別コンテキスト検品、merge後の照合 |

維持する条件は、現在のWorkspace/Project/固定参照の認可、送信禁止分類とScope/除外、初期からの有界取得、既存の操作要求回数・MCP回数・期限・費用の照合、原応答と欠測の保存、秘密値の非公開、クライアント操作とRun/Hopper/Triageの帰属分離である。ChatGPT接続の出力token上限なしという本人判断も維持する。画面や設定の表示だけでは推論しない。

## UIの判断

1. Workspaceの利用者が、既存のクライアント接続または許可されたサーバー接続とモデルを選ぶ。
2. 既存のクライアントLLM設定と各操作のモデル選択を使う。接続候補が全体、選んだ接続のモデルと料金が詳細で、新しい画面やペインは増やさない。
3. Webへサーバー接続の取得・選択を追加する。選択中は直結用のURL・キー・提供元ログインを要求しない。直結接続の保存・認証、native操作と保存データの意味は変えない。

本人へ手動UI検査を依頼しない。操作・切替は実handlerを通す自動試験で判定し、ブラウザ実表示が使えない場合は未確認の対象と理由を記録する。以前拒否されたoriginを別の操作経路で迂回しない。

## 最終形と検証

既存の`operationPort`とクライアントHostの送信意図を使い、サーバー接続を選んだ場合だけGoの共通Gatewayへ中継する。資格情報と接続先は運用者の既存設定から取得し、モデル送信にOry/MCP cookie/tokenを転送しない。サーバーで送信を一度だけ予約し、観測した応答を既存のfinish経路へ渡す。原記録とブラウザへ返す共通応答を分ける。

ChatGPTのResponses変換は共通責務へ集約する。既存の単発のJSON評価に加え、assistantの履歴、名前空間付きfunction tool、call/outputと推論itemを扱い、`store=false`・`stream=true`、terminal完了の判定を維持する。toolの実行権限は既存のMCP読取りに限定したままとする。

局所検証は`serverllm`と`clientops`のGo試験、専用DBの`test:clientops`、共通Host/設定のVitestと型、関連Hopper/Triage試験とbuildとする。共有の新規経路を所有fixtureで確認し、実アカウントの推論と混同しない。認証済みの個人接続を実消費する試験は、本人の個別指示なしに行わない。個人環境の配備記録はGit管理外に置く。

調査の終了条件は、既存begin/finishと送信予約の保存方法、共通tool履歴の仕様、UIの共有呼出元を確認し、ACに必要な一本の送信経路へ絞ること。未使用の基盤や別の対話エンジンを追加しない。

公式の根拠は[SIWCのモデルと推論](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)、[preview制約](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)、[function calling](https://developers.openai.com/api/docs/guides/function-calling)と[Responses仕様](https://developers.openai.com/api/reference/resources/responses/methods/create)である。

## 現在の状態

実装と独立検品を終え、[PR #91](https://github.com/KingYoSun/koujou-san/pull/91)の最終CIを確認している。最終的なCI・merge後照合の結果は同PRへ集約する。接続登録・認証の新しい経路や、クライアント不在時の自動実行は追加していない。

| 条件 | 局所の結果 |
| --- | --- |
| AC-1 | Web設定の静的描画、Workspace別の候補取得、直結接続と料金設定の維持、専用transport以外で送信しない検査が成功。各操作のモデル選択に再取得ボタンを設けた |
| AC-2 | 他人・他Workspace・入力改変・接続版変更・Project送信禁止を上流0回で拒否。送信後の権限取消と担当引継ぎでは採用せず原応答を保持 |
| AC-3 | 同じturnの並列要求と完了後の再要求で上流は1回。ブラウザの偽finishを拒否し、通信切断を失敗として保存。クライアントHostはサーバー確定済みの操作を再取得し、二重のfinishを送らない |
| AC-4 | 所有SSEで通常対話、名前空間付きtool往復、暗号化された推論itemの継続、未完了・失敗・切断を検査。共通応答のassistant role欠落を自動試験で確認して修正。既存Gateway経路もHTTP入口から成功 |
| AC-5 | 実DBでチャット回答とContext選択を保存し、Run件数を維持。利用量のprotocol、報告token数、重複なし、追加本文が一覧に出ず固定readに残ることを確認 |
| AC-6 | 手順とADRを更新。固定commitの独立検品は完了。最終CI・merge後の照合をPRで確認する |

`go test ./...`、`go vet ./...`、`test:clientops`と同suiteの`GOFLAGS=-race`、`test:hopper`、`test:automation`、clientの型検査とWeb全体Vitest、root build・Prettier・Rust整形・`git diff --check`が成功した。最終固定commit以降の追加変更は、その影響範囲を再検査する。

rootの`check`は、既存のGit管理外`apps/server/.cache/issue33-config/main.go`が未整形のため停止した。そのファイルは変更せず、後続のGo vet・client型・Prettier・Rust整形を個別に実行した。全体checkはclean CIで補完する。

UIは静的描画と実handlerによる自動試験で確認した。1440px／1024px・focus・横スクロールのブラウザ実測は未確認。以前拒否されたoriginを別経路で観測せず、本人への手動検査も再依頼しない。レイアウト・CSSの変更はない。実ChatGPT認証・実推論も実施していない。

独立検品者`web_server_models_audit`は、会話履歴を継承しないコンテキストで`76018712`を検品した。送信予約後、同じ担当クライアントによるモデル変更があった場合に、旧モデルへ送信してから拒否するAC-2の未達を1件確認した。新しいDB試験で修正前に上流呼出しが発生することを再現し、送信前のclaimでも現在モデルとturnのモデルを照合するよう修正した。未完了応答が許可外モデルの拒否理由を上書きしない検査も追加し、修正前失敗・修正後成功を確認した。ブラウザ側にも予約の現在モデルと選択中の接続版を照合するAPI transportの試験を追加した。修正後の対象UI試験・型検査とrace付きDB suiteは成功。

`a079fe0e`の差分再検品ではblocker解消・追加blockerなし。AC-1〜5をPASS、AC-6は独立検品完了・CI待ちと判定した。独立再実行でもGo対象3パッケージ、Vitest対象2ファイル17件、型検査・差分検査が成功した。

PR #91は最終CIの全checkが成功し、`4a25981e`でmergeした。配備前の[Responses schema](https://developers.openai.com/api/reference/resources/responses/methods/create)の再照合で、Namespace toolの必須`description`欠落を確認したため、AC-4を再開した。既存の所有fixtureがこの必須項目を検査していなかった。検査を追加して修正前の失敗を確認し、共通変換へ読取りtoolの説明を付けた。変更は当該一項目と回帰検査に限定し、追加PRで検品・CIを確認してから配備する。個人環境への切替はまだ行っていない。

同じ配備前検査で、継続用の`server_output`がturn一覧へ残ることを追加DB試験で確認した。AC-5の追加原履歴を一覧から除く条件として、サーバー接続のturn一覧からこのtransport用のfieldも除去し、固定readでの保持を検査した。既存の直結・nativeのturnと、固定readを使うHostの継続は維持する。この差分も[補完PR #92](https://github.com/KingYoSun/koujou-san/pull/92)で検品する。
