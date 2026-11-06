# Projectチャットの相談開始と状態確認

## 範囲と判断

基準commitは `f59418f`。開始時の作業ツリーに変更なし。ユーザーの直接依頼と、2026-11-06の回答を根拠とする。
Scope revision 2。追加回答で履歴の自動参照と実環境更新を承認。

- 相談ごとの画面を維持する。ChatGPTを参考に、最初の送信で相談を作り、Project・LLM接続の選択を引き継ぐ。
- 登録設定、実行状態に加え、登録リポジトリ内のREADME・コードも相談から参照する。
- 追加回答により、同じ相談の直近の発言を自動で含める。直近20件から完了した発言を固定版で選び、送信範囲を入力欄で確認・除外できる。別相談や全履歴は自動取得・送信しない。
- 会話本文、確認中の状態、参照結果を中心に表示し、固定版・原応答・仕様採用は詳細から開く。

チャットからのファイル編集・任意command実行、新しいRunの自動開始、Slackのチャンネルへの保存構造変更は対象外。
登録リポジトリの参照先とcommitを明示し、サーバー上の登録先の内容と端末の未commit変更を混同しない。

## 変更前の証拠

- WindowsのNAHTLOKA Workspaceで「初期リリース」Projectの既存相談を確認。検索・スレッド作成・接続選択のフォームが並び、会話以外の領域が大きい。
- AIの返答にある `###`、`**`、表がそのまま文字列で表示される。`project-chat.tsx` は本文を単一の `p` 要素に置く。
- `send` は作成済みスレッドと選択済みモデルを要求する。Projectとモデルは初期値が空のコンポーネント状態。
- `client-selection-host.ts` と `clientops.ReadTool` は、chat目的にContext用の4道具しか許可しない。MCP認証成功だけでは設定・実行状態・リポジトリファイルを読めない。
- 変更前の `chat-operation-controls.test.tsx`、`client-selection-host.test.ts`、`navigation-flows.test.tsx` は24件成功。

## 条件と作業

| ID | 対象・期待結果 | 予定path | 判定方法 |
| --- | --- | --- | --- |
| AC-1 | 初回は利用可能なProject・モデルを選べる。再訪時は同じAPI・利用者・Workspaceの選択を復元する。Projectが一つなら検索操作を要求しない | clientのProjectChatと選択状態 | 初回・再訪・Workspace切替の操作テスト |
| AC-2 | 新しい相談は本文入力と送信で始まる。題名入力・スレッド作成を要求せず、最初の本文から題名を付ける。既存相談へ戻って継続できる | ProjectChat、chat-api | フォームから保存・モデル応答までのテスト。失敗時の同じ要求IDによる再確認 |
| AC-3 | 返答の見出し・リスト・表・コード・強調・リンクを読める。本文と進行状態を同じ会話の流れに置き、詳細操作を必要時に開く | ProjectChat、本文表示、styles.css、workspace-shell | Markdown表示・危険なHTMLを実行しないテスト、1440px・1024pxの実表示 |
| AC-4 | 接続済み・未認証・確認中・失敗を区別する。相談のLLMにProjectの設定と実行状態を確認する道具を渡し、取得結果・未取得・参照範囲を区別して回答する | client-selection-host、clientops、mcpapi | MCPの実tool往復、未接続・権限不足・空設定・取得成功の試験 |
| AC-5 | 選択Projectの登録リポジトリから、ディレクトリと指定ファイルを必要な範囲で読む。返答に参照先・commit・取得範囲を示す | repositories、clientops、mcpapi | 管理下のリポジトリHTTPと実DBでREADME・コード取得、ページ継続、別Project拒否を確認 |
| AC-6 | NAHTLOKAで、製品UIから開発準備の相談を開始し、設定・実行状態・登録ファイルの参照結果と不足設定への導線を確認する | 共通UIと登録接続 | 製品操作の観測。補助スクリプトによる設定代行を受入証拠にしない |
| AC-7 | 同じ相談の直近20件から完了した発言を自動で選び、次の送信に含める。送信範囲を表示し、別相談・未完了・過去ページの全履歴は自動で含めない | ProjectChat | 連続送信、再訪、過去ページ表示、別相談への切替と送信内容の試験 |
| INVAR-1 | 共有の固定版・現在認可・送信禁止分類・要求ID・原応答と利用量の記録を保つ。既存会話と仕様採用を引き続き読める | 既存の保存・操作・MCP境界 | 関連回帰テストと独立検品 |
| INVAR-2 | Context・Project・履歴・ファイルは必要なページと範囲だけ取得し、全件取得を開始条件にしない | 一覧・検索・読取り | 複数ページの取得条件と通信記録を照合 |
| INVAR-3 | 閲覧・接続・再表示でLLMの再送やRun開始を行わない。Webへ端末のCLI実行能力を追加しない | 操作Hostと復帰処理 | 既存の中断・結果不明テスト、操作経路の確認 |

## 画面の責務

1. 開発者が相談を開始し、返答の根拠と次に必要な設定を確認する。
2. 左はProject切替と相談履歴、中央は会話と入力欄、右は選んだメッセージ・採用・原応答の詳細。会話画面では全機能の長いナビゲーションを折りたたむ。
3. 題名入力と手動のスレッド作成を通常導線から除く。モデルは現在値を入力欄の近くに表示し、切替・認証を必要時に開く。既存の共有保存形式と過去の選択メッセージの固定参照を再利用する。

## 実装調査の終了条件

- リポジトリの登録・接続サービスを再利用できる読取り経路を特定し、現在認可とProjectの一致を検証できたら調査を終える。
- 状態確認に必要な登録・Runner・Runの情報の取得元と、取得時点・未取得の扱いを固定する。別目的の設定機能は追加しない。
- UIの旧導線を置換し、同じ責務を持つ新旧画面を残さない。保存と認証の共通経路は再利用する。

## 検証

区分C。LLMへの読取り情報と認可経路を変更するため、固定した差分と条件で独立検品を行う。
関連UIテスト、共通UIの型検査・build、関連Goテスト、専用DB/MCP試験、整形と差分検査を行う。
ネイティブ境界を変更した場合はdesktopのcheck/testとWindowsの該当操作を追加する。
UIは1440pxと1024pxで読み順・本文表示・focus・横はみ出しを確認する。

## 現在の判定

In progress。AC-1〜5とINVAR-1〜3のローカル実装・検証・独立検品を完了した。AC-6のNAHTLOKA実データでの確認は、共有サーバーとWindowsアプリの更新後に行う。現在稼働中の環境へはまだ反映していない。

| 条件 | 結果と証跡 |
| --- | --- |
| AC-1・2 | Projectがある状態で本文から相談を作り、Project・モデル・相談を復元。`project-chat-flow.test.tsx` で最初の送信、保存ACK喪失後の同一要求再確認、再訪時の非送信、未接続時の保存・送信拒否を確認 |
| AC-3 | `chat-markdown.test.tsx` で見出し・表・リスト・コード・リンクとHTML非実行を確認。Chromeの合成データ画面で1440×1000／1024×1000を操作し、横幅は1440/1440、1009/1009。履歴の折りたたみ、相談作成、詳細ペインの開閉を確認。画像は `.cache/chat-preview/result-1440.jpg` と `result-1024.jpg` |
| AC-4・5 | 実Ory・MCP SDK・専用PostgreSQLと管理下のGitea HTTPで、設定4種、固定commitの一覧とREADME、UTF-8継続、別Project拒否、参照無効時の拒否、現在の送信禁止方針を確認。要求Rangeと提供元が送信するbytesも検証 |
| INVAR-1〜3 | 既存のchat保存・仕様採用・送信意図・応答喪失・利用量・再開、別利用者・Workspace、None入力と送信禁止分類の試験が成功。新しいtoolから外部書込みやCLI開始へ到達する処理なし |

実行した確認:

- `corepack pnpm run check`、`corepack pnpm run test`、`corepack pnpm run build`：成功。共通UIは160件成功、専用環境を必要とするセットアップ試験1件は通常実行時skip。
- `pnpm run test:clientops`：専用DBで成功。
- `pnpm run test:mcp`：実Ory・SDK・検索エンジンを含む試験で成功。1万Context×3版のページ取得も確認。
- Goの関連パッケージテストと `go vet ./...`、整形、`git diff --check`：成功。
- 通常の `pnpm run check/test/build` は、このWindows環境のpnpm.exeをroot wrapperがNode.jsの入力として起動するため失敗した。同じ10.16.1のCorepack経由で全体検査を完了。最初のcheckで検出した既存の未追跡 `.cache/issue33-config/main.go` はgofmtで整形した。製品コードへの環境対策は加えていない。
- Web buildの500kB超chunk警告は残る。TauriのRust実装は変更していない。稼働中のWindowsアプリへの適用と実NAHTLOKAでのモデル相談は未実施。

独立検品は会話履歴を継承しない別担当で、バックエンド13ファイルとUI15ファイルのSHA-256を固定して実施した。ファイル継続時の既読部分の再取得をRangeへ変更し、MCPの未認証・通信失敗・再確認中の混同を修正した。状態試験は修正前の2件失敗と修正後の成功を確認。両担当の再検品はblockerなし。UI担当は関連4テストも独立実行して成功した。

2026-11-06の追加回答で履歴の自動参照と現在環境への更新が承認された。AC-7を追加して範囲を固定し、更新候補を作り直してからAC-6を確認する。

AC-7は通常の連続送信、70件の履歴からの限定取得、未完了・別相談の除外、過去ページ表示後の範囲維持、入力欄からの除外を確認した。独立検品で送信途中の再取得が再試行入力を変える問題を確認し、送信中だけ履歴選択を保持するよう修正。20件の履歴でモデル開始に失敗した後も同じpolicy本文・要求IDを使う試験は修正前に失敗し、修正後に成功した。固定3ファイルの再検品はblockerなし、関連4テストも独立実行して成功。

## 更新候補

ローカルcommit `f945f7abbbabc70815219d61f2ea503368cb77b0` から、`corepack pnpm run dist:desktop` と `corepack pnpm run dist:server` を実行して成功した。既存のRust実装を含むWindows release buildとNSIS作成、Linuxサーバーimage・archive作成を完了。外部へのpush・PR・配備は行っていない。

- Windows: `artifacts/desktop-0.1.0-preview.1-f945f7ab/koujou-san_0.1.0-preview.1_x64-setup.exe`。SHA-256: `3e9eea513d6a4a804f4c920ed5b542c5666bd0e87575fcf84e311bae4a4a026b`。
- サーバー: `artifacts/server-0.1.0-preview.1-f945f7ab/koujou-san-server-linux-amd64-0.1.0-preview.1-f945f7ab.tar.gz`。SHA-256: `94c4586a647d759e620d64272eb14be9776fa0d9e9e229547777f7c87df2e98f`。既存schema 28を維持。

接続先は現在 `https://koujou.kingyosun.com`。共有サーバーの更新には一時停止とbackup、Windows版の更新には現在のRunnerの状態確認とアプリ終了が必要になる。初回観測時のアプリには未送信入力があったため、更新の実施判断をユーザーへ確認する。更新候補の作成はNAHTLOKAの受入確認の代わりにはしない。

参照資料: [Slackのスレッド](https://slack.com/help/articles/115000769927-Use-threads-to-organize-discussions)、[サイドバー](https://slack.com/help/articles/212596808-Adjust-your-sidebar-preferences)。Slackのチャンネル構造は採用せず、相談ごとの画面を維持する判断に更新した。

実装資料: [react-markdown公式](https://github.com/remarkjs/react-markdown)、[GiteaのGit treeページ取得](https://docs.gitea.com/enterprise/api/21/operations/get-tree/)。
