# Issue #27: Bifrostの隔離接続試験

2026-10-18（JST）。現在の判定は[current-state.md](current-state.md)、実装範囲は[計画](2026-10-18-issue-27-plan.md)を参照する。この試験は候補のHTTP境界を確認したもので、Hopperの意味判断・発行や実providerの成功には数えない。

## 固定した対象

公式[リポジトリ](https://github.com/maximhq/bifrost)のHTTP release `transports/v2.3.0`、commit `9237ec06672406e437b3cb33fc793ffd0403b3ae`を使用した。GitHubのlatest releaseはCLI版を指していたため、HTTPのreleaseを区別した。[公式の要求オプション](https://docs.getbifrost.ai/providers/request-options)と固定sourceのschema・認証handlerを参照した。cloneに`.codegraph/`はなく、通常の検索を使った。

imageは`maximhq/bifrost:v2.3.0`、digestは`sha256:8f37e33a8caa85faf59a875605c61d1d934738e517d8fe96e2d9fcf6f0ef9098`。sourceのライセンスはApache-2.0。同梱構成の全依存・配布通知は実装時に確認する。

## 所有する試験環境

専用Docker project `koujou-issue27-gateway-probe`にGatewayとNodeの合成providerを置き、internal networkで外部への通信を隔離した。価格表・モデル情報も合成providerへ向けた。実Context・実APIキー・既存CLI認証は使っていない。認証は試験専用の管理者とvirtual key。MCP libraryとlive modelの定期同期を無効にし、Warp・semantic cache・Maxim・OTELを有効化しない構成とした。telemetry pluginのactive表示は残り、この表示から外部送信なしを推測しない。

| 対象 | 観測 |
| --- | --- |
| 管理と推論の認証 | 未認証の管理APIと推論はともに401。管理者ログイン後はcookieで管理APIを参照できる |
| schemaと固定モデル | strict JSON schemaが上流へ届き、`owned-alpha`のJSONと実モデル名が戻る |
| 障害とfallback | `owned-fail`単独は503、明示した`openai/owned-beta`へのfallbackだけが200。暗黙fallbackは行われなかった |
| retryと相関 | `max_retries:0`。再起動前の上流はalpha一回、失敗二回、beta一回。応答の`x-bifrost-request-id`は指定した要求IDと一致 |
| usage | 成功応答にinput 11/output 7/total 18 token。これは合成の報告値であり、実費ではない |
| 本文ログ | `disable_content_logging:true`、object storageへの保持と要求別上書きを無効。ログAPIと所有SQLiteの5ログに入力・出力マーカーがないことを確認 |
| 再起動 | 専用SQLiteの設定を保ち、同じvirtual keyとモデルへの要求が200 |

最初のhost側health取得はinternal networkの公開portへ到達せず、fixtureのstartup timeoutとなった。隔離network内のproviderコンテナからHTTPを実行するよう変更した。管理ログインのtokenがJSONにあるというfixtureの仮定も誤りで、実装どおりcookieを使って成功した。Gateway本体の修正はしていない。DB確認の初回はmigration行も件数に数えて上限へ達したため、schema履歴を除いたログ関係テーブルだけを読み直した。

成功後に専用コンテナとnetworkを停止・削除した。所有する設定・SQLite・試験コードはGit管理外の`.cache/issue27-bifrost-probe/`へ残した。ログと要約は`test-results/issue27-gateway-probe-final.log / issue27-gateway-probe-final.json / issue27-gateway-storage-check.json`。

probeのSHA-256は`6dda1a3f46df697dcf9de028816dbaf65a372adee2b844198f3b2a11eefe0483`、結果JSONは`db96f4dd905e2ddde1fec873bb830c9be001824a6ca6f4326269278e45072c5d`。この調査結果と本人の回答により任意サービスとして同梱する方針が確定した。共有業務処理・設定画面・更新復旧・配布の受入は後続の実装で確認する。
