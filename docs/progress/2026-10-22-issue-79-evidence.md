# Issue #79: eMudhra中間CA漏えいへの対応

## 作業範囲

- 受入条件: [Issue #79](https://github.com/KingYoSun/koujou-san/issues/79)、Scope revision 1。AC-1、AC-2、INVAR-1を変更しない。
- 基準commit: `224478a92a73058ff944c6a5985b8f004e5e3412`。開始時の作業ツリーに変更なし。
- 区分C。Node、Go、Bifrost、Kratos、Hydra、標準配布のCAとWindowsのnative-tls経路を対象とする。機能追加、データ移行、無関係な依存更新、過去の侵害調査は対象外。
- 本人の回答により、このPCだけで開発・検証しており、遠隔配備先はない。

## 作業と判定方法

| 条件 | 作業 | 確認方法 |
| --- | --- | --- |
| AC-1 | Node最低版・CI固定版・最低版チェック・手順を22.23.4以上へ揃える | 設定の照合、実行版、検証サイトの12経路で証明書エラー、通常HTTPSの成功 |
| AC-2 | Go 1.27.3以上でAPI・edgeを再ビルドし、外部イメージと配備CAを更新する | archiveの公式checksum、バイナリのbuild情報、コンテナ内の版・CA、再起動後の12経路、配布archiveの照合 |
| INVAR-1 | 通常HTTPS・認証・モデル接続・明示的なカスタムCAを維持する | 関連テストと実操作。拒否試験には秘密値を一切渡さず、証明書検証を無効にしない |

Oryの公式修正済みイメージを確認できなかったため、同版ソースの再ビルド・同梱案と公式版を待つ案を本人へ示した。本人は「公式の修正イメージを待つ（Oryの対応は未完了のまま残る）」を選んだ。Oryを自前ビルドへ切り替えず、AC-2は未完了として残す。CAは利用者の明示設定を保存する現在の契約に従い、ホストの更新が既存のコピーへ反映されたとは扱わない。

局所検証は変更した開発コマンド、Goの配布・TLS関連テスト、配布build、Docker Compose設定、NodeとWindowsのTLS実操作。全体の回帰はPR CIで確認する。固定commit、条件、観測結果を、実装会話を継承しない別担当へ渡して独立検品する。

## 変更前の証拠

Issue本文に記録されたNode固定22.14.0、Go 1.27.2、Bifrost v2.3.0、Ory v26.2.0を開始時の設定と照合した。実行中のNodeはすでに22.23.4だった。Dockerでは開発用Bifrost v2.3.0と、開発用・試験用のKratos／Hydra v26.2.0が稼働していた。

## 検証結果

### Nodeと開発環境

Node最低版を22.23.4、Goを1.27.3へ更新し、CI両job、Go module、配布notice、手順を揃えた。Goの6種類のarchive名とSHA256を公式配布一覧と照合し、Windows実行版も1.27.3へ更新した。旧Go 1.27.2はGit管理外に保全した。

変更前の`doctor`入口は22.23.3を誤って受理した。変更後は最低版をpatchまで比較し、22.14.0・22.23.3・23.0.0を拒否、22.23.4・22.24.0を受理した。実行中のNodeは22.23.4で、setupとdoctorは成功した。standaloneの`pnpm.exe`をNodeで読み込もうとする既存制約があるため、コマンドは`corepack pnpm run ...`で実行した。

最低版の再現コマンドは次のとおり。実行版は変えず、`process.versions.node`だけを22.23.3として入口を呼ぶ。変更前は受理され、変更後は終了コード1と`Use Node 22.x >= 22.23.4`で拒否された。

```powershell
node --input-type=module -e "Object.defineProperty(process.versions, 'node', {value: '22.23.3'}); process.argv[2] = 'doctor'; await import('./scripts/dev.mjs')"
```

### NodeとWindowsのTLS

対象は`dv / ov / ev` × `g3b / g3c` × 通常・AAAクロス署名の12経路。Node 22.23.4 / OpenSSL 3.5.9は12本すべて`CERT_REVOKED`で、TLS接続の確立前に拒否した。`example.com`は200、`nodejs.org`は307を返し、通常のTLSは成功した。

Windows 11 Pro 10.0.26300で、Tauriの`auth.rs`から`build_client`をそのまま抽出したprobeを、同じreqwest 0.13.5・native-tls 0.2.18・schannel 0.1.30で実行した。12本すべて失効エラー`0x80092010`、通常HTTPSは成功した。所有するloopback HTTPS fixtureはCA未指定で拒否され、既存の`KOUJOU_DEV_CA_FILE`を指定すると200になった。fixtureへのHTTP到達数は0→1で、OS信頼ストアを変更していない。

検証先へ資格情報・実データを送っていない。timeoutやDNS失敗を拒否として数えていない。詳細JSONとprobeはGit管理外の`artifacts/issue79-tls/`、toolchainの照合は`test-results/issue-79-toolchains.json`に保存した。

### GoとBifrost

Go 1.27.3のWindows／Linuxで、APIの実際の`serverllm.Connection.Call`と`distribution.Edge`を使ったprobeを実行した。モデル処理は12本すべて`ErrUnavailable`、edgeは503となり、同じTLS設定の補助要求で12本すべてがx509証明書検証エラーであることを確認した。LinuxはAlpine `ca-certificates 20261020-r0`のbundleを`SSL_CERT_FILE`で指定し、モデルの明示`CAFile`でも同じ拒否を確認した。通常HTTPSと、所有HTTPSモデルへの明示カスタムCA接続、edgeへの追加CA接続は成功した。

Bifrostはv2.3.2、Go 1.27.3、Alpine 3.23.6、CA `20261020-r0`へ更新した。固定digestは`sha256:f5b18826349fe9e1c402c10d19f1cbb5567a3c800ad928580d07f72e69106ec7`。更新前v2.3.0では通常6経路がTLSを通過してHTTP 405となり、AAA6経路は拒否された。更新後の実バイナリを明示再起動すると12本すべてをx509エラーで拒否した。合成鍵での通常モデルAPI到達は401、明示カスタムCA付きの所有HTTPSモデルは200だった。

開発用Bifrostも停止・DB退避後に同じvolumeを保持して更新し、healthyを確認した。更新前後の`config.db`は66テーブルの行数が一致した。DB退避、12経路の応答、再起動後の結果はGit管理外の`.cache/issue79-images/`に保存した。Go probeとBifrostで確認した公開CA bundleのSHA256は`5961c0a3c65459931c023e0075c09733f09aea4ca439cb85bc4a08bb4f3f568b`。

Ory Kratos／Hydraの公式releaseとDocker Hubの最新更新順を確認したが、修正済みイメージは見つからなかった。稼働するv26.2.0は双方ともGo 1.26.0、CA `20250911-r0`だった。本人の判断に従い変更せず、公式修正版の固定digest更新・切替・再起動・各12経路の検証を残す。

### 配布物

コード候補`d6d74b6dd0393d51d71cda85fd79fd98cc092223`から`corepack pnpm run dist:server`が成功した。LinuxのAPI・edge・管理CLIはいずれも`go version -m`でGo 1.27.3、同候補のrevision、`modified=false`を確認した。配布manifestの9ファイルのchecksumと、ロードしたimage IDは一致した。Oryは公式版待ちのため、この配布物を標準構成全体の対策完了とは扱わない。

WSL Ubuntu 22.04の新規所有fixtureへこの配布物を導入した。`init --ca`には旧bundleと所有するloopback証明書を渡し、原本だけを更新しても配備コピーが変わらないことを確認した。その後`stop`→修正bundleと同じカスタムCAで`config/ca.pem`を差し替え→`up`を実行し、API・edge・Kratos・Hydraの再起動を確認した。コピー先とAPI／edgeから見えるCAのSHA256は`2213ef1e4c66f9d84eb3971dbc2fae5ec71805f1e63c2f54e0587c10936a085e`で一致した。

APIとedgeの各コンテナで配布済み`/app/edge check`を実行し、12本すべての拒否、`example.com`の成功、カスタムCAを使う自サイトの成功を確認した。モデル通信の証明書エラー理由は前述の実`Connection.Call`のprobeと対応付けた。既存の`test-distribution.py`でbootstrap、二Userの登録・所有メール検証・BFFログイン・招待受諾、固定データ・履歴・所属・関係、Bleve／ArcadeDBの結果と反映位置も成功した。所有fixtureは最後に停止した。

配布試験のharnessは、`ServeEdge`内部の`service unavailable`を期待して一度停止した。配布入口が`edge unavailable`へ抽象化する実装を確認して期待値を訂正した。製品側の変更はなく、初回の試験失敗も`distribution-results.json`へ保存した。統合結果は同ファイル、認証は`distribution-auth.log`、build情報とchecksumは`distribution-*-build-info.txt`と`distribution-build-hashes.json`に保存した。

### 局所検証

`corepack pnpm run test`はGoの全packageと共通UIの105テストが成功した。配布packageのGoテスト、管理対象Goファイルのgofmt、`go vet ./...`、共通UIの型検査、Prettier、Cargo fmt、`git diff --check`も成功した。`corepack pnpm run build`も成功し、Windows用APIをGo 1.27.3で再ビルドした。

`corepack pnpm run check`自体は、開始前から存在するGit管理外の`apps/server/.cache/issue33-config/main.go`のgofmtで失敗した。そのファイルは変更せず、同じ検査の各段階を分けて実行し、gofmtの対象だけをGit管理対象へ限定した。未成功のrootコマンドを成功とは記録しない。

## 判定と残作業

今回の更新と局所検証は完了した。AC-1の実行環境と12経路、AC-2のGo・Bifrost・配布CA、INVAR-1の通常HTTPS・モデル・認証・明示CAを確認した。独立検品は[検品記録](2026-10-22-issue-79-inspection.md)、最終CIとmerge後の照合は[PR #80](https://github.com/KingYoSun/koujou-san/pull/80)へ集約する。

AC-2全体はOryの公式修正版待ちで未完了。Kratos／Hydraを公式修正済みのGo・CAを含む固定digestへ切り替え、実体の版・CA・再起動後の12経路を確認するまで#79をCloseしない。現在の判定は[current-state.md](current-state.md)を参照する。

## 参照した公開情報

- [インシデント・12経路・各CA修正版](https://emudhra.we-have-a-problem.com/)
- [Node 22.23.4](https://github.com/nodejs/node/releases/tag/v22.23.4)
- [Goの修正版発表](https://groups.google.com/g/golang-announce/c/66pojueaQek)、[公式archiveとchecksum](https://go.dev/dl/?mode=json)
- [Bifrost v2.3.2](https://github.com/maximhq/bifrost/releases/tag/transports%2Fv2.3.2)
- [Kratos公式release](https://github.com/ory/kratos/releases)、[Hydra公式release](https://github.com/ory/hydra/releases)
