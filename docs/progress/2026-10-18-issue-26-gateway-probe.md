# Issue #26: Manifestの予備接続検証

2026-10-18時点。Context選択Hostは実装・接続検証済み。Manifestの採用は保留し、Tauriに直接組み込むSDKを比較した。本文ログの初期無効は確定しており、Gatewayを標準構成へ追加していない。

## 固定した候補

Manifest `manifest@6.28.9`、source `f36dfa739af2a993d1dfaca10f59ddc2df58d60b`、image `manifestdotbuild/manifest:6.28.9@sha256:a60dae190aa622f2b0be25a5583c3361a647ce85de46fca16631a6a0a9ede2a6`。公式リポジトリとDocker手順をFirecrawlで取得し、同じtagのsourceをGit管理外へ浅く取得した。研究用checkoutにはCodeGraph索引がないため通常検索を使った。

公式資料: [README](https://github.com/mnfst/llm-gateway/blob/f36dfa739af2a993d1dfaca10f59ddc2df58d60b/README.md)、[Docker手順](https://github.com/mnfst/llm-gateway/blob/f36dfa739af2a993d1dfaca10f59ddc2df58d60b/docker/DOCKER_README.md)、[LICENSE](https://github.com/mnfst/llm-gateway/blob/f36dfa739af2a993d1dfaca10f59ddc2df58d60b/LICENSE)。rootはMITの表示がある。依存・配布物の追加確認と残る判断は後述する。

## 所有環境で確認した事実

独立したDocker projectにPG、固定Gateway、合成上流HTTPを起動し、毎回生成した所有アカウントと接続キーだけを使った。実プロバイダ資格情報・実LLM・実Contextは使っていない。終了時にprojectを限定してcontainer/networkを削除した。標準開発サービスには触れていない。

`test-results/issue26-manifest-probe.json`と`issue26-manifest-probe-extended.json`に資格情報を含めない観測を保存した。二つのモデルでtool要求・構造化出力とusageが通り、明示したモデルが所有上流へ届いた。定義外のモデルは上流へ送られず、失敗モデルも別モデルへ切り替わらなかった。この構成では上流の失敗要求は一回だった。配置したGatewayを再起動しても設定と生成キーは使えた。

許可したWeb Originのpreflightは通り、別Originへ許可ヘッダは返らなかった。これはHTTP/CORSの観測であり、実ブラウザのTLS・Private Network AccessやTauri全経路の確認には代用しない。

本文記録を無効、Autofixを全体で無効、匿名利用統計と更新確認を無効にして起動した。PGの試行記録は本文保存先を持たず、traceparentのIDと操作用session key、実モデル、試行数を確認できた。モデル価格情報の取得など、その他の外部読取りまで無通信とは扱わない。追加の送信観測と限界は後述する。

## Hostで必要な拒否

未登録モデルはHTTP 200の通常応答形式で`model=manifest`、M302の説明文を返した。HTTP成功だけでは推論成功と扱えない。Hostは実モデルと結果schemaを検証し、この応答を選択結果として反映しない。固定版sourceの明示モデル解決も、未知モデルから既定モデルへ切り替えない経路だった。

`issue26-manifest-probe-extended.log`は、未知モデルが必ずHTTPエラーになるとした初期fixtureの失敗を残す。`issue26-manifest-probe-extended-2.log`では、応答本文と上流件数を照合して成功した。この試験調整をGateway不具合の修正と記述しない。

## 追加確認と採用判断

追加の所有試験では、既定ルートを失敗モデルに固定し、許可した一件のfallbackから`owned-beta`の応答を得た。上流の失敗試行と切替先を別件として数え、明示モデルへの要求には暗黙のfallbackがないことを照合した。本文ログを有効にした合成要求には保存先が付き、再び無効にした次の要求には付かなかった。`test-results/issue26-manifest-probe-final.json`に固定版、相関、試行、本文保存の有無を保存した。生成キーは証跡へ保存していない。

同じ試験では、起動前にNodeのfetch観測器を読み込み、起動・要求・失敗・fallback・再起動の期間を観測した。Autofixとtelemetryへのfetchはなく、合成本文を持つPOSTは所有する上流の`provider`だけだった。外部へのGETは`openrouter.ai`、`models.dev`、`modelparams.dev`、`raw.githubusercontent.com`の価格・モデル・資料の取得だった。これは当該期間のfetchの観測であり、全通信方式や将来の全期間の無通信を保証しない。固定版sourceでも、設定無効時にAutofixの送信・probeとtelemetryの定期処理を戻すことを確認した。

固定imageをネットワークなしで読み、実行時のNodeパッケージ354件を列挙した。licenseメタデータはMIT 277、Apache-2.0 35、ISC 20、BSD-2-Clause 7、BSD-3-Clause 3、BlueOak-1.0.0 4、0BSD 1、MPL-2.0またはApache-2.0の選択1、MITとBSD-3-Clauseの併用1、未記載5だった。未記載はManifestのprivate package三件と`busboy / streamsearch`であり、後二件には当該image内でLICENSEファイルも見つからなかった。記録は`test-results/issue26-manifest-runtime-licenses.json`。rootのMIT表示だけで、imageやOSを含む全配布物の通知義務を確認済みとは扱わない。再配布する構成を選ぶ場合は配布物の通知・ライセンス同梱を確定する必要がある。

候補の必要機能と導入負担の有限な調査は終えた。その後のユーザー指示により、Manifestの採用を保留して[Tauri組込みSDK](2026-10-18-issue-26-native-llm-comparison.md)を比較した。採用方針は判断待ちであり、本文ログは初期無効とする。モデル二件のプロトコル合成試験を、実LLMによる意味判断の品質の成功へ読み替えない。候補のsource改造や新しいGateway製品の探索へ広げない。

固定した所有試験のSHA-256は次のとおり。どちらにも生成キーは含めない。

| 対象 | SHA-256 |
| --- | --- |
| `.cache/issue26-gateway-probe.mjs` | `561d031fa9e5de59f7db43a2f7b06b8b7ec898172056650e667cb94abab517d3` |
| `test-results/issue26-manifest-probe-final.json` | `b7e04dc21cbf6893882c8bfe26f1ac548924fef47c108cde5cce307fb2abdcfe` |
| `test-results/issue26-manifest-runtime-licenses.json` | `3bd7af0c6dd71b8c3b07250ef8b216265b577ba8dcab511bdc16361a49732497` |
