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

### NodeとWindowsのTLS

対象は`dv / ov / ev` × `g3b / g3c` × 通常・AAAクロス署名の12経路。Node 22.23.4 / OpenSSL 3.5.9は12本すべて`CERT_REVOKED`で、TLS接続の確立前に拒否した。`example.com`は200、`nodejs.org`は307を返し、通常のTLSは成功した。

Windows 11 Pro 10.0.26300で、Tauriの`auth.rs`から`build_client`をそのまま抽出したprobeを、同じreqwest 0.13.5・native-tls 0.2.18・schannel 0.1.30で実行した。12本すべて失効エラー`0x80092010`、通常HTTPSは成功した。所有するloopback HTTPS fixtureはCA未指定で拒否され、既存の`KOUJOU_DEV_CA_FILE`を指定すると200になった。fixtureへのHTTP到達数は0→1で、OS信頼ストアを変更していない。

検証先へ資格情報・実データを送っていない。timeoutやDNS失敗を拒否として数えていない。詳細JSONとprobeはGit管理外の`artifacts/issue79-tls/`、toolchainの照合は`test-results/issue-79-toolchains.json`に保存した。

### 局所検証

`corepack pnpm run test`はGoの全packageと共通UIの105テストが成功した。配布packageのGoテスト、管理対象Goファイルのgofmt、`go vet ./...`、共通UIの型検査、Prettier、Cargo fmt、`git diff --check`も成功した。

`corepack pnpm run check`自体は、開始前から存在するGit管理外の`apps/server/.cache/issue33-config/main.go`のgofmtで失敗した。そのファイルは変更せず、同じ検査の各段階を分けて実行し、gofmtの対象だけをGit管理対象へ限定した。未成功のrootコマンドを成功とは記録しない。

配布buildと後続のTLS検証を追記する。現在の判定は[current-state.md](current-state.md)へ集約する。

## 参照した公開情報

- [インシデント・12経路・各CA修正版](https://emudhra.we-have-a-problem.com/)
- [Node 22.23.4](https://github.com/nodejs/node/releases/tag/v22.23.4)
- [Goの修正版発表](https://groups.google.com/g/golang-announce/c/66pojueaQek)、[公式archiveとchecksum](https://go.dev/dl/?mode=json)
- [Bifrost v2.3.2](https://github.com/maximhq/bifrost/releases/tag/v2.3.2)
