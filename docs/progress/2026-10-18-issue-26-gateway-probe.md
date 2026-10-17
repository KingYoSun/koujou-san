# Issue #26: Manifestの予備接続検証

2026-10-18時点。製品への採用・同梱は未確定。Context選択Host自体も未実装である。

## 固定した候補

Manifest `manifest@6.28.9`、source `f36dfa739af2a993d1dfaca10f59ddc2df58d60b`、image `manifestdotbuild/manifest:6.28.9@sha256:a60dae190aa622f2b0be25a5583c3361a647ce85de46fca16631a6a0a9ede2a6`。公式リポジトリとDocker手順をFirecrawlで取得し、同じtagのsourceをGit管理外へ浅く取得した。研究用checkoutにはCodeGraph索引がないため通常検索を使った。

公式資料: [README](https://github.com/mnfst/llm-gateway/blob/f36dfa739af2a993d1dfaca10f59ddc2df58d60b/README.md)、[Docker手順](https://github.com/mnfst/llm-gateway/blob/f36dfa739af2a993d1dfaca10f59ddc2df58d60b/docker/DOCKER_README.md)、[LICENSE](https://github.com/mnfst/llm-gateway/blob/f36dfa739af2a993d1dfaca10f59ddc2df58d60b/LICENSE)。rootはMITの表示がある。依存・配布物全体の確認は未完了。

## 所有環境で確認した事実

独立したDocker projectにPG、固定Gateway、合成上流HTTPを起動し、毎回生成した所有アカウントと接続キーだけを使った。実プロバイダ資格情報・実LLM・実Contextは使っていない。終了時にprojectを限定してcontainer/networkを削除した。標準開発サービスには触れていない。

`test-results/issue26-manifest-probe.json`と`issue26-manifest-probe-extended.json`に資格情報を含めない観測を保存した。二つのモデルでtool要求・構造化出力とusageが通り、明示したモデルが所有上流へ届いた。定義外のモデルは上流へ送られず、失敗モデルも別モデルへ切り替わらなかった。この構成では上流の失敗要求は一回だった。配置したGatewayを再起動しても設定と生成キーは使えた。

許可したWeb Originのpreflightは通り、別Originへ許可ヘッダは返らなかった。これはHTTP/CORSの観測であり、実ブラウザのTLS・Private Network AccessやTauri全経路の確認には代用しない。

本文記録を無効、Autofixを全体で無効、匿名利用統計と更新確認を無効にして起動した。PGの試行記録は本文保存先を持たず、traceparentのIDと操作用session key、実モデル、試行数を確認できた。モデル価格情報の取得など、その他の外部読取りまで無通信とは扱わない。Autofix・telemetryの外向き通信の完全な観測は未完了。

## Hostで必要な拒否

未登録モデルはHTTP 200の通常応答形式で`model=manifest`、M302の説明文を返した。HTTP成功だけでは推論成功と扱えない。Hostは実モデルと結果schemaを検証し、この応答を選択結果として反映しない。固定版sourceの明示モデル解決も、未知モデルから既定モデルへ切り替えない経路だった。

`issue26-manifest-probe-extended.log`は、未知モデルが必ずHTTPエラーになるとした初期fixtureの失敗を残す。`issue26-manifest-probe-extended-2.log`では、応答本文と上流件数を照合して成功した。この試験調整をGateway不具合の修正と記述しない。

## 残る有限な確認

許可したfallback経路、再試行の計測・応答喪失との相関、本文ログの切替・外向き通信、固定版の依存と配布、実ブラウザ/Tauri transport、Context選択との一巡をAC-8/9で確認する。モデル二件のプロトコル合成試験を、実LLMによる選択品質の成功へ読み替えない。候補のsource改造や新しいGateway製品の調査は、この予備検証へ追加しない。
