# サーバーのChatGPTプラン接続

## 目的と範囲

Hopper・TriageのサーバーLLM処理で、ChatGPTへの専用OAuthログインとResponses APIを使えるようにする。Codex CLI、Codex SDK、個人の既存認証ファイルを使わない。基準commitは`aa954668597c421839534e406e639fdd717226e6`。開始時のcheckoutに差分はない。区分Cとして、認証・秘密値・外部送信・保存の経路を独立検品する。

2026年11月2日の本人判断により、ChatGPTプラン接続の出力トークン上限を撤回する。入力サイズと時間の上限は維持する。既存HTTP Gateway接続の出力上限は変更しない。サブスク枠の消費量を事前に制限する機能とは扱わない。

| 条件 | 対象と期待結果 | 判定方法 |
| --- | --- | --- |
| AC-1 | 管理用コマンドで専用SIWC登録・再認証・モデル一覧・切断ができる。state・PKCE・nonce・署名・issuer・audience・subject・利用scopeを検証し、未検証の情報を有効な認証として保存しない | 所有するOIDC/JWKS/HTTP fixtureと一時credentialによる成功・拒否・再認証試験 |
| AC-2 | credentialは接続ごとにprivateなファイルへ原子的に保存し、同じファイルのrefreshを直列化する。再ログインをモデル実行中に開始せず、トークン・パス・他Workspaceの許可を共有メタデータへ返さない | 期限切れ・並列refresh・置換・メタデータ・権限・失効の試験。WindowsとLinuxの保護処理も検査 |
| AC-3 | サーバーは許可した接続とモデルで公式Responses APIへ直接送信する。`store=false`と`stream=true`、`response.completed`による完了判定を守る。未完了・失敗・切断・時間切れを成功にしない | 所有するSSE fixtureで実要求・原応答・実モデル・usage・terminal状態・redirect非追跡を照合 |
| AC-4 | Hopper・Triageが新接続を選べる。ChatGPT接続では出力上限を設けず、既存API接続では既存上限を保つ。固定入力・予約・原応答・採用判定・利用量を既存の共通処理へ接続する | 共通transport試験と専用DBのHopper/Triage試験、型検査、静的描画と対象フォームの表示確認 |
| AC-5 | 標準配布に認証用コマンドとcredentialの書込み用mount・backupを接続する。専用認証と自己ホスト時の操作が分かる | Compose設定、配布・backupの局所試験、実行コマンド、一般化した手順書の確認 |

維持する条件は、既存HTTP接続と保存済み定義の互換性、Workspaceと資源の認可、送信禁止分類、モデル許可、原応答と利用量の保存、Contextの固定参照・有界取得、秘密値の非公開、既存RunのCLI実行との分離である。サブスク利用をAPI従量課金の実額や費用0へ変換しない。

対象外は、Claude等の別provider、Codexランタイムの組込み、WebからのCLI実行、ChatGPT会話の取得、外部APIキーへの自動fallback、実アカウントの無断ログイン・実消費、個人環境の設定の登録である。ChatGPT接続のfallbackは空で使い、既存Gatewayのfallbackは維持する。

## 最終形と確認

Goの共通認証部品でSIWCのファイル保存・更新と管理コマンドを扱い、`serverllm`の接続種別で既存GatewayとResponsesを選ぶ。業務側の意味判断や採用処理を複製しない。元のResponses streamと実際の送信要求を保持し、モデル・usageを共通の判定形式へ読み取る。

画面ではWorkspace管理者が接続とモデルを選び、入力・時間の上限と出力上限なしを判断する。既存の中央の設定フォームと接続一覧に種別を表示し、新しい画面や遷移は追加しない。ChatGPT接続の出力token入力を「上限なし」へ置き換え、従来のAPI接続の入力欄は維持する。

局所検証はGoの認証・transport・配布試験、Hopper/Triageの専用DB試験、共通UIの型・描画・実表示、rootのcheck/test/buildとする。固定差分と証拠を別コンテキストへ渡して検品し、PR CIの結果も確認する。本人認証が必要な実接続は、fixtureの成功と区別して未確認のまま記録し、許可された接続と消費枠が揃うまで実行しない。

## 参照した公式仕様

- [SIWC登録とログイン](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- [accountとrefresh](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)
- [モデルと推論](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [self-hosted VM](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms)
- [preview制約](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)

## 現在の状態

実装中。出力上限についての本人判断を受領し、範囲を確定した。新しい製品判断が必要になった場合は、その判断に依存する変更を止める。
