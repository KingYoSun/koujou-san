# 開発環境の構築記録

- 日付: 2026-10-15（JST）
- 依頼: kukuriの構成・指示書を参考に、Issueやユーザーの指示から実装できる環境を用意する
- 対象: `codex/development-environment` のローカル成果物
- 判定: Complete（環境構築。製品の段階Aは未実装）

## 用意したもの

Go APIとReact・Tauriの共通UI、pnpm／Cargoのworkspaceとlockfile、公式checksum付きGo SDKの準備、ランダムな開発用秘密値の生成、環境診断と検証コマンドを揃えた。Node 22.14.0、pnpm 10.16.1、Go 1.27.2、Rust 1.98.1で確認した。システムのGo 1.19.4は置き換えていない。

PostgreSQL 17.11、ArcadeDB 26.10.1、Ory Kratos・Hydra v26.2.0の開発用Composeを用意した。製品・Kratos・HydraのDBとroleを分け、Oryのmigration後にサービスを起動する。公開ポートはloopbackへ限定し、データはnamed volumeで保持する。

AGENTS・CLAUDEの入口、計画指針、Issueから実装・検品・終了までの手順、Issue／PR様式、計画・検品記録の雛形を用意した。Codexのworktreeにも同じsetupを登録した。CIはLinuxのGo・共通UIとWindowsのnative検証を定義している。

## 条件と証拠

| 条件 | 結果と確認 |
| --- | --- |
| AC-ENV-1 | PASS。指示書・手順・Issue／PR様式から、直接指示とIssueの両方で開始できる。登録コマンド、文書リンク、YAML／TOML構文を照合 |
| AC-ENV-2 | PASS。setup、doctor、check、test、build、check:desktop、test:desktop、build:desktopを実行。setup再実行で.env保持を確認。最終版のtarによるGo展開も確認 |
| AC-ENV-3 | PASS。APIのhealth応答と、ChromeでWebの「起動を確認しました」を確認。ブラウザのエラー・警告なし。Windows release executableでプロセスとタイトルkoujou-sanのnative window起動を確認 |
| AC-ENV-4 | PASS。Compose設定、4サービスhealthy、2migration終了コード0、ArcadeDB・Kratos・HydraのreadinessとOIDC discoveryを確認。DB roleの非superuser・相互DBへのCONNECT拒否、down→up後のデータ保持を確認 |
| INVAR-ENV-1 | PASS。設計書のGit差分なし。kukuriはcleanのまま。既存のkukuriコンテナを変更していない |
| INVAR-ENV-2 | PASS。秘密値・個人設定・SDK・生成物をignore。別プロジェクトの.env・個人設定・権限は複製していない。CodeGraph索引を新規作成していない |

実行の要約はローカルの `test-results/*.json`、CI実行後の証跡はworkflowのartifactへ残る。ネイティブテストは現在の薄いwrapperのコンパイル・テストtarget実行であり、業務機能のnativeテストはまだ存在しない。

Windowsの最初のnative検査では必須のICO不足を検出した。SVGから必要なアイコンを生成して登録し、以後のコンパイル・テスト・release buildは成功した。release linkerにDLL import library生成の情報メッセージがwarningとして出たが、build終了コードは0だった。

## 独立検品

実装の会話履歴を自動継承しない別担当へ、リポジトリ、今回の依頼、受入条件、成果物を渡して検品した。setup/task処理、toolchain、CI、APIとUI、Compose、秘密値と権限の分離を対象とし、具体的blockerは0件だった。

検品担当もGoテスト、UI型検査・6テスト、スクリプト・Compose構文を独立実行し、4サービスhealthyと2migration終了コード0を直接観測した。Codex設定とnativeアイコンの参照も確認した。検品時のSHA-256の先頭は `scripts/dev.mjs: E28B5B8F`、`infra/compose.yaml: 51CBC8EE`、`tauri.conf.json: FE348EA5`。レビュー後の変更は記録と指示書の文言整合のみである。

## 実行範囲と残る確認

確認環境はWindows端末とDocker DesktopのLinuxサービスである。GitHub上のCIはまだ起動していない。Linux・macOSのnative build、署名・配布、native画面の視覚検品は今回の実行範囲に含めない。Windowsのnative起動確認はプロセスとwindowの生成までで、各製品機能の動作確認とは区別する。

ログイン・Workspace・Run・Runner・検索投影・同期・MCP・LLM接続は未実装。次の実装範囲は[現在状態](current-state.md)と設計書§14・§18から定める。

検証用のAPI・Web・nativeプロセスと、本タスクのComposeサービスは停止した。SDK・依存・Docker image・永続volume・ローカル.envは次の作業に使える状態で保持した。コミット、push、PR、GitHub Issue作成は行っていない。

Go展開確認用の `.cache/go-extraction-smoke` の再帰削除は、自動承認レビューにポリシーでブロックされたため実行していない。この一時コピーはGit管理外に残っている。
