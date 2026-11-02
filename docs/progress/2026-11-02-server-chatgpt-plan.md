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

実装と独立検品を終え、対象フォームの実表示と[PR #90](https://github.com/KingYoSun/koujou-san/pull/90)のCIを確認している。実装本体は`749e26f`、追加の再認証・復元試験は`3ec7675`、一覧への本文混入の修正は`2b128e5`へ固定した。実accountへの認証・推論は未実施。実表示が未確認のため、PRはdraftを維持する。

| 条件 | 観測結果 |
| --- | --- |
| AC-1 | 所有RSA署名・JWKS・callback・token endpointで登録と再認証が成功。誤state・nonce・audience・subject・署名と利用scope欠落を拒否。専用Bearerによるモデル一覧で非表示モデルを除外 |
| AC-2 | Windowsのprivateな一時credentialを使用。期限切れへの6並列要求でrefreshは1回。token回転後も接続revisionを維持し、account変更では旧revisionを拒否。公開メタデータに秘密値・path・他Workspaceの許可を含めない |
| AC-3 | 所有SSE fixtureで完了・失敗・未完了・切断・時間切れ・許可外モデルを検査。送信本文、原stream、報告model・usageを保存。出力12345 tokenも従来の4096制限へ切り詰めず、追加要求やredirect先への転送なし |
| AC-4 | 専用PostgreSQLのHopper/Triage試験で新接続から業務結果と利用量を保存。型検査・静的描画2件は成功。実表示は下記のとおり確認待ち |
| AC-5 | Composeの資格情報用RW mountと通常設定RO mountを照合。設定の再生成で最新tokenを維持し、所有archiveからhost・client登録・回転済tokenを復元。既存snapshotがconfig配下を再帰収録する経路を確認 |

主な検証結果は次のとおり。

- `go test ./...`、`go vet ./...`、`go test -race ./internal/chatgptplan ./internal/serverllm ./internal/distribution -count=1`は成功。
- `pnpm run test:hopper`、`pnpm run test:automation`は新しいChatGPT fixtureのDB試験を含め成功。
- clientの`check`、対象`server-model.test.tsx`、rootの`build`・`format:check`、`cargo fmt --all -- --check`、Git管理対象Goファイルのgofmt、`git diff --check`は成功。
- rootの`check`は、既存のGit管理外`apps/server/.cache/issue33-config/main.go`が未整形のため停止。そのファイルは変更せず、後続検査を個別実行した。
- UI全体試験は未変更の`sync-store.test.ts`2件が5秒でtimeout。他32ファイル118件は成功。同期ストアの単独実行でも同じ2件がtimeout。対象変更の試験成功とは区別し、CIで全体結果を確認する。
- Windowsのstandalone `pnpm.exe`をNodeとして起動する既存helperで最初のroot buildは失敗。同じ固定版`pnpm@10.16.1`のJS入口をGit管理外cacheに用意し、root buildの成功を確認した。配布helper自体の変更は行わない。

UIの実表示は、実データ・外部通信のない固定フォームを用意したが、ブラウザのorigin承認が拒否されたため未確認。別経路での観測は行わず、本人へAPI→ChatGPT→APIの切替と1440px／1024pxの表示確認を依頼した。回答まではAC-4の実表示を達成済みとしない。

別コンテキストの独立検品で、`Response.Request`と`Response.Reply`がHopper/Triageの一覧に残るAC-4の回帰を確認した。追加したDB試験が修正前に両方のheads一覧で失敗した。Hopperの共通圧縮処理とTriageのheads・records・attemptsのSQLで、原応答に加えて送信本文・解析応答も除去した。固定記録では三つとも保持する。修正後はHopperのheads・history、Triageの三経路の検査を含む両DB suiteが成功した。

独立検品者`chatgpt_plan_audit`は、会話履歴を継承しないコンテキストで`749e26f`と`3ec7675`を検品し、上記1件をblockerとした。`2b128e5`の差分再検品で解消を確認し、追加blockerはなしと判定した。AC-1/2/3/5はPASS、AC-4は実装・回帰修正PASS、実表示を含む条件全体はINCONCLUSIVE。CI成功やfixture成功を実表示・本人認証の代わりにはしない。
