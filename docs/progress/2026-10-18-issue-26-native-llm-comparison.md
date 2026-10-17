# Tauri内のLLM接続候補: rigとrust-genai

2026-10-18のユーザー指示に基づく調査。Manifestの採用を保留し、Tauriへ直接組み込む二候補を、subscription認証とagent的な動作も含めて比較した。比較後、ユーザーはrig-core＋rig-agentの直接組込みを選択した。以下は候補比較時の結果であり、組込みの契約は[ADR 0026](../adr/0026-client-context-selection.md)、実施結果と実契約の確認状況は[証跡](2026-10-18-issue-26-evidence.md)へ記録する。本文ログの初期無効も確定した。

## 結論

tool反復の実行制御とsubscription認証を重視するなら、`rig-core + rig-agent`を第一候補とする。プロバイダのHTTP抽象だけを置き換え、既存Hostの制御を保つなら、rust-genaiの方が責務は小さい。どちらも外部のGatewayプロセスや専用PostgreSQLを必要とせず、RustライブラリとしてTauriへリンクできる。

ただし、ライブラリにOAuthの実装があることと、koujou-sanで各社の月額契約を利用できることは別に判定する。ユーザーの実契約でログイン・推論はしていない。CLIや既存アプリの秘密ファイルも利用していない。Claude Pro/Maxの認証を第三者アプリへ転用する方式は、公式の利用範囲に合わないため採用候補に含めない。

## 固定した調査対象

| 候補 | 公開版 | 公開版のsource | 別途読んだ開発中source |
| --- | --- | --- | --- |
| rig | `rig-core 0.44.0 / rig-agent 0.44.0`、MIT、Rust 1.95以上 | `2d1071e0e0c602f994e9d6cfd8cf5faea6f53650` | `c73622dfca02bfe55d29a6a0612e181119cab9b8` |
| rust-genai | `genai 0.7.0-rc.8`、MITまたはApache-2.0 | `8d9ee1248605ec885031b447a466737671abb0d9` | `aceafb8bc37a4199d7aff92dd12f816096025207`、`0.7.0-rc.9-WIP` |

crates.ioの公開tarballを取得し、`.cargo_vcs_info.json`と実装を照合した。READMEの最新版だけで公開版の機能を判断していない。取得した二つの研究用checkoutにはCodeGraph索引がないため、通常検索を使った。候補比較だけの段階では、製品の依存・lockfileを変更しなかった。

## 機能と組込み

| 観点 | rig-core + rig-agent | rust-genai |
| --- | --- | --- |
| 責務 | coreはモデル・メッセージ・provider、agentはtool反復・履歴・hook・上限・中断再開 | provider間の要求・応答・usage・stream・tool入出力を共通化。実行ループは呼出側が持つ |
| agent的な動作 | 組込みループに加え、I/Oのない`AgentRun`を`CallModel / CallTools / Done`ごとに進められる | `exec_chat`→tool結果→`append_message`→再要求をアプリで実装する。toolを返すだけで自動実行はしない |
| 原応答・usage | モデル応答のrawとusage、agentの各モデル呼出しを取得できる | 非streamは`capture_raw_body`を明示でき、rawと正規化usageを取得できる |
| 構造化出力 | 型付きextractorとschemaを扱う。providerの能力確認は必要 | ChatOptionsのresponse formatとJSON schemaを扱う。providerの能力確認は必要 |
| MCP | 許可したMCP呼出しをtool実装へ結べる。業務認可をSDKへ移さない | 同じくMCPの認証・呼出しはアプリが接続する |
| Tauri | Windowsの所有HTTP試験でビルド・実行成功。フルCLI、harness、Bevyを組み込む必要はない | 同じWindows試験でビルド・実行成功。API呼出しだけを差し替えやすい |
| Web | core/agentにWASMの考慮はあるが、公開版のChatGPT OAuthはWASMでは非対応 | 今回はnativeのみ確認。ブラウザ実行を確認済みとしない |

根拠: [公開版rig-agentの契約](https://github.com/0xPlaygrounds/rig/blob/2d1071e0e0c602f994e9d6cfd8cf5faea6f53650/crates/rig-agent/README.md)、[rigのtool反復例](https://github.com/0xPlaygrounds/rig/blob/c73622dfca02bfe55d29a6a0612e181119cab9b8/examples/agent_run_stepping/src/main.rs)、[genaiのtool往復例](https://github.com/jeremychone/rust-genai/blob/8d9ee1248605ec885031b447a466737671abb0d9/examples/c20-tooluse.rs)、[genaiのraw応答](https://github.com/jeremychone/rust-genai/blob/8d9ee1248605ec885031b447a466737671abb0d9/src/chat/chat_response.rs)。

## subscription認証

| 契約・方式 | rig公開版0.44.0 | genai公開版0.7.0-rc.8 |
| --- | --- | --- |
| ChatGPTの契約枠 | 専用provider、device-code OAuth、token更新、account ID、Codex向けResponses処理がある | 専用のログイン・更新フローは見つからない。custom endpoint/authで独自に接続する余地はあるが、対応済みとはしない |
| GitHub Copilot | device-code OAuthとCopilot用token交換の実装がある。実契約での試験は未実施 | `github_copilot`という名前のadapterは、実際には`models.github.ai/inference`と`GITHUB_TOKEN`を使うGitHub Models向け。Copilot subscriptionのログインとして扱わない |
| Z.ai Coding Planなどの専用API | coding用endpointを持つ。契約用のキーと対象APIが必要 | `zai_coding`などの専用endpointがある。一般の月額契約とAPI利用を一括して同じ方式にしない |
| Claude Pro/Max | 通常のAnthropic APIを使う。Claude.aiの月額OAuthをkoujou-sanへ転用しない | 同左 |

rigの[ChatGPT認証](https://github.com/0xPlaygrounds/rig/blob/2d1071e0e0c602f994e9d6cfd8cf5faea6f53650/crates/rig-core/src/providers/chatgpt/auth/native.rs)と[Copilot認証](https://github.com/0xPlaygrounds/rig/blob/2d1071e0e0c602f994e9d6cfd8cf5faea6f53650/crates/rig-core/src/providers/copilot/auth/native.rs)は公開client IDと専用送信先を実装している。第三者アプリとして提供する条件・認証の登録方法・実契約での利用可否を、この実装の存在だけで確認済みにしない。

[OpenAIの認証文書](https://developers.openai.com/codex/auth)は、CodexのChatGPT契約でのログインと、APIキーの従量課金を区別している。取得した公式文書では、独立したkoujou-sanが同じOAuth clientを提供できるという根拠までは得られなかった。ChatGPTの契約枠を製品機能として確約する前に、この点と実ログインを確認する。

[Anthropicの認証・資格情報の公式説明](https://code.claude.com/docs/en/legal-and-compliance#authentication-and-credential-use)は、第三者アプリによるClaude.aiログインの提供や、ユーザーのFree/Pro/Max資格情報を使う中継を許可せず、APIキーまたは対応cloud providerを案内している。既存のRunが利用者自身の未改変Claude Codeを動かすことと、クライアントLLMとしてOAuth資格情報を取り込むことを混同しない。

## 暗号化保存の差

製品組込み後、本人の許可を得たChatGPT契約試験では、新しいbrowser PKCE、専用DPAPI保存、`gpt-6.1-sol`の二要求・一tool往復・JSON取得、端末内削除・提供元の失効が成功した。[実接続の証跡](2026-10-18-issue-26-evidence.md#chatgpt契約での実接続)を参照する。これは上表の公開版比較とは別の観測であり、Copilotや他のモデルの実契約確認には数えない。

rig 0.44.0のOAuth保存先は、指定ファイルへのJSON保存である。WindowsのDPAPI保存をそのまま満たさない。開発中sourceには`TokenStore`と明示的なbrowser/device sign-in・sign-outがあり、DPAPI実装を接続できるが、これを公開版の機能として数えない。

genaiは`AuthResolver / ServiceTargetResolver`で、取得済みtokenや接続先を渡せる。DPAPI、ログイン画面、更新、失効はkoujou-san側の責務として残る。いずれも、Ory/MCP・Run用CLIの認証と保存先を分離する。CLIの既存tokenを探して流用する方式は採らない。

根拠: [rig 0.44.0の保存処理](https://github.com/0xPlaygrounds/rig/blob/2d1071e0e0c602f994e9d6cfd8cf5faea6f53650/crates/rig-core/src/providers/internal/auth/device.rs)、[開発中のTokenStore](https://github.com/0xPlaygrounds/rig/blob/c73622dfca02bfe55d29a6a0612e181119cab9b8/crates/rig-core/src/providers/internal/auth/store.rs)、[genaiの接続先・認証resolver](https://github.com/jeremychone/rust-genai/blob/8d9ee1248605ec885031b447a466737671abb0d9/examples/c06-target-resolver.rs)。

## 所有環境での試験

Windows、Rust 1.98.1で、製品と独立したCargo projectを`.cache/issue26-sdk-probes/`に用意した。依存を上記の公開版へ固定し、不活性キー`owned-key`とloopback HTTPだけを使った。実provider・実契約への要求、実Context、個人認証の読み込みはない。

`cargo run --manifest-path .cache/issue26-sdk-probes/Cargo.toml`が成功した（初回build 2分7秒、`test-results/issue26-sdk-probes.log`）。rig-agentでは2回のモデルHTTP要求の間に1回の所有toolを実行した。genaiでもアプリが明示した2回の要求でtool結果を返し、最終応答とraw bodyを取得した。両方の受信側でOry/MCP cookie・headerがないことを検査した。これをTauri本体への組込み、実OAuth、実LLMの意味判断品質の成功とは扱わない。

## 採用時に維持するもの

Go APIの現在権限・禁止分類・固定版・receipt・操作履歴と、クライアントによるモデル実行を維持する。SDKの自動tool実行やretryを無条件に有効化せず、各`CallModel`の前に送信意図・上限を確定し、原応答とusageを同じ操作へ戻す。共有形式をSDKの内部checkpointへ置き換えない。

Tauriの直接接続を採用する場合も、Webからnative OAuthやOS資格情報を使えることにはしない。Webの既存HTTP接続を残すか、提供機能を変えるかは利用者体験の判断となる。候補比較の有限な調査はここで終え、認証・保存・画面を置き換える前に採用方針を確認する。
