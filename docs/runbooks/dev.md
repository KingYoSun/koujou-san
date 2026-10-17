# 開発手順

コマンドはリポジトリのrootで実行する。作業の開始・検品・終了は[Issueから実装する手順](issue-lifecycle.md)に従う。

## 初回セットアップ

Node.js、pnpm、Git、Rustを用意する。採用版はrootの`package.json`、lockfile、Rustのtoolchain設定を参照する。ネイティブの開発には各OSのTauri用依存、標準サービスの起動にはDocker Composeが必要になる。

```powershell
pnpm run setup
pnpm run doctor
pnpm run check
pnpm run test
```

`setup` は開発用の `.env` と依存関係を準備する。ローカルの秘密値はランダムに生成し、Go 1.27.2はリポジトリ内の `.tools/go` 配下へ置いて開発コマンドから使う。ローカルの設定・依存・生成物はGit管理しない。`doctor` の結果で利用可能なツールと不足を確認する。

`pnpm setup` はpnpm自体の組込コマンドなので、このリポジトリの準備には `pnpm run setup` を使う。

`.codegraph/` の新規作成はsetupに含めない。索引を作る場合はユーザーが選び、その後の探索は `AGENTS.md` に従う。

Codexのworktree向けに `.codex/environments/environment.toml` のsetupにも `pnpm run setup` を登録している。モデル・実行権限・個人の認証設定はこのファイルで上書きしない。アプリの設定からsetupや操作入口を変更できる。仕組みは [OpenAIの公式資料](https://learn.chatgpt.com/docs/environments/local-environment) を参照する。

## 日常コマンド

| コマンド                 | 対象                                           |
| ------------------------ | ---------------------------------------------- |
| `pnpm run doctor`        | 開発環境の確認                                 |
| `pnpm run check`         | Goと共通UIの静的検査                           |
| `pnpm run test`          | Goと共通UIのテスト                             |
| `pnpm run test:database` | 試験専用PostgreSQLの保存・並列更新・停止・復元 |
| `pnpm run test:auth` | 試験専用Ory・DB・メール配送による認証・復旧・失効 |
| `pnpm run test:mcp` | 実Ory・SDK・検索エンジンによるMCP認証・操作境界・編集 |
| `pnpm run test:mcp:ui` | 二UserでContext編集を実操作する検証環境 |
| `pnpm run test:access` | 試験専用DBによるWorkspace・端末の認可と失効 |
| `pnpm run test:runs` | 試験専用DBによるRun入力・二Pause・原出力・派生判定・export |
| `pnpm run test:workflows` | 試験専用DBによる版付き定義・工程進行・入力・判断・予算・外部条件 |
| `pnpm run test:automation` | 試験専用DBによる受理・永続Worker・Triage方針と優先度 |
| `pnpm run test:usage` | 試験専用DBによる利用量・料金版・費用評価・認可・集計 |
| `pnpm run test:github` | 試験専用DBと固定応答によるGitHub観測・権限・復旧 |
| `pnpm run test:github:live` | 明示したGitHub Appによる実接続の読取り |
| `pnpm run test:github:ui` | 実Appと専用Ory・DBによる共通UIの観測表示 |
| `pnpm run test:contexts` | 試験専用DBによるContext・所属履歴・基準版・入力確定 |
| `pnpm run test:sync` | 試験専用DBによる共有同期・競合・履歴不足・報告順序 |
| `pnpm run test:search` | 試験専用DBとBleveによる検索・認可・反映・再構築 |
| `pnpm run test:graph` | 試験専用PG・ArcadeDBによる関係探索・反映・入力確定・復旧 |
| `pnpm run test:graph-load` | 合意したWindows環境での関係探索負荷測定 |
| `pnpm run test:search-load` | 合意したWindows環境での検索品質・負荷測定 |
| `pnpm run build`         | GoとWebのbuild                                 |
| `pnpm run check:desktop` | Tauri側のコンパイル確認                        |
| `pnpm run test:desktop`  | Tauri側のテスト                                |
| `pnpm run build:desktop` | ネイティブアプリのbuild                        |

通常の `check / test / build` にはネイティブの検証を含めない。Tauriの変更では対応するdesktopコマンドを選ぶ。`build:desktop` はOSの依存を必要とし、実行時間も長くなる。

## 開発サーバー

別々のターミナルで必要なものを起動する。

```powershell
pnpm run dev:api
```

```powershell
pnpm run dev:web
```

既定のAPIは `https://localhost:8080`、共通UIは `https://localhost:1420` で起動する。認証の初期化、開発用TLS、メール確認は[認証の開発手順](authentication.md)に従う。ネイティブ側を確認するときは `pnpm run dev:desktop` を使う。各開発プロセスはターミナルの `Ctrl+C` で停止する。設定を変更した場合は起動ログで接続先を確認する。

Go APIは起動時にDBへ接続してmigrationを実行する。標準PostgreSQLを先に起動する。接続先・readiness・実DB試験・backupの手順は[保存と復旧](persistence.md)を参照する。

## 標準サービス

```powershell
pnpm run infra:up
pnpm run infra:status
pnpm run infra:down
```

構成は `infra/compose.yaml`、初期化と接続先は[標準サービスの手順](../../infra/README.md)を参照する。PostgreSQL、ArcadeDB、Ory Kratos・Hydraなどのサービスをローカルの開発用に扱う。BleveはGo側の検索コンポーネントであり、独立したComposeサービスは前提にしない。

サービスの起動と、アプリからの認証・更新反映・検索の接続は別の確認になる。サービスがhealthyでも製品機能が動く証拠にはしない。`infra:down` は通常の停止に使い、永続データの削除は必要な対象と目的を確認してから行う。

秘密値を含むローカル設定は `.env` 等のGit管理しないファイルへ置く。共有する雛形へ実際の資格情報を記入しない。別プロジェクトの `.env` をコピーしない。

## 変更に対応する検証

| 変更                                  | 選ぶ検証                                                                    |
| ------------------------------------- | --------------------------------------------------------------------------- |
| `apps/server/**`                      | 関連Goテスト、`pnpm run check`。API変更は利用側との対応も確認               |
| `apps/client/**` の共通UI             | 関連UIテスト、型検査。表示や操作の変更は対象画面の実操作も確認              |
| `apps/client/src-tauri/**`            | `pnpm run check:desktop`、関連ネイティブテスト。OS依存の挙動はそのOSで確認  |
| `infra/**`                            | Composeの構文・設定の確認。起動が条件にある場合は対象サービスだけを起動確認 |
| `scripts/**`、toolchain、lockfile、CI | 変更したコマンドと依存解決、対象CIの構文・実行結果                          |
| 文書・コメント                        | 記述、リンク、例示コマンド、`git diff --check`                              |

表は全コマンドを毎回実行する指定ではない。受入条件と差分から検証を選び、全体の回帰確認にはPR CIを使う。ローカルで不足するOSや外部接続は、必要な検証と実行先を先に記録する。未実行の検証を、別の検査が成功したことだけで成功にしない。

実機確認は変更したOS境界や自動化できない挙動に絞る。共有UIは有効な共通画面の証拠を再利用し、WebとTauriで同じ操作を機械的に繰り返さない。本人の操作が必要な確認は必要最小限にまとめ、既存の証拠で条件を満たせる場合は追加依頼しない。

不具合では変更前の失敗と変更後の成功を同じ条件で確認する。実装方法の写しになるテストを増やさず、利用者に見える結果と必要な副作用を検証する。文書の確認のために、例示された公開・配備・データ削除を実行しない。
