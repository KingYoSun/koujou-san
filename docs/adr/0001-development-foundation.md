# ADR 0001: 実装開始用の開発環境

- 状態: 採用（開発環境の構成のみ）
- 日付: 2026-10-15（JST）
- 根拠: `koujou-san-design-doc.md` §1.1、§6.7、§14、§18と、kukuriの開発手順・指示書

## 判断

Go APIを `apps/server`、Web・Tauriで共有するReact UIを `apps/client` に置く。TauriのRustコードは `apps/client/src-tauri` とし、rootのCargo workspaceで検証する。共通UIはネイティブのCLI実行APIに依存しない。

Nodeによる `scripts/dev.mjs` をローカルとCIの共通入口とする。pnpm 10.16.1、Node 22.x（最低22.23.4）、Rust 1.98.1、Go 1.27.3を使う。Go SDKは公式アーカイブのSHA-256を検証して `.tools/go` に展開する。対応する固定版がシステムにある場合も使用できる。システムのSDKを置き換えない。

標準サービスは開発用Composeに置く。PostgreSQLの製品・Kratos・HydraのDBとroleを分け、ArcadeDBのデータは別の永続volumeへ置く。秘密値は新規のローカル `.env` へ生成し、サービスの公開ポートはloopbackに限定する。BleveはGoへ組み込むライブラリなので、架空の外部サービスは置かない。

通常のcheck/test/buildはGoと共通UIを対象とする。Rustの整形検査はcheckに含め、OS依存のnativeコンパイル・テスト・buildはdesktopコマンドで選ぶ。CIはLinuxでGo・共通UI、Windowsでnativeも確認する。

Issueまたは直接の指示を作業の入口とし、有限な受入条件、維持条件、確認方法、結果を記録する。独立検品の深さはこの開発リポジトリの手順として定義する。kukuriの個人設定にある外部書き込み権限、P2P仕様、一条件一PRを製品の共通仕様へ移植しない。CodeGraph indexingは自動実行しない。

## 今回確定しないこと

Run・Context・Scopeの詳細schema、同期・認可・Runner、BleveのAnalyzerと配置、ArcadeDBのquery方式、MCPの版・SDK、Oryの業務フロー、Gateway採否、配布構成、OSSライセンスは後続のIssueで検証・決定する。開発用のサービス起動は段階Aの完成を意味しない。

## 参照

- [Goの公式配布とchecksum](https://go.dev/dl/)
- [ArcadeDBの起動](https://docs.arcadedb.com/arcadedb/tutorials/run.html)
- [Ory Hydraのデータベースとmigration](https://www.ory.com/docs/hydra/self-hosted/dependencies-environment)
- [Tauriの開発前提](https://v2.tauri.app/start/prerequisites/)
