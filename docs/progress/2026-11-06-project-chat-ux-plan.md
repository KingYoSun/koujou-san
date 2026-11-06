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

Complete。AC-1〜7とINVAR-1〜3の実装・検証・独立検品を完了し、共有サーバーとWindowsアプリへ反映した。NAHTLOKAの実データで設定・登録ファイルの参照と、履歴を自動で含めた会話の継続を確認した。NAHTLOKA自体の開発準備完了を意味するものではない。

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

実環境の受入中、MCP認証後の最初のdiscoveryが失敗した。配布edgeが公開Hostを維持してlocalhostのAPIへ転送する構成に、SDKのlocalhost向けHost検査が合っていなかった。loopback接続と公開Hostを組み合わせた試験で403を再現し、設定済み公開Hostとの明示照合へ変更。未設定・別Hostは拒否し、既存のOAuth、操作認可、Origin検査は維持する。モデル要求はこの失敗の時点ではまだ送っていない。修正後のサーバーで同じ相談を再開して確認する。

公開Host修正後、MCP discovery・道具一覧の取得は成功したが、Windowsのモデル接続の許可リストが新しい2種類の読取り道具を拒否した。ネイティブ境界の試験で `project_inspect` 拒否を再現し、`project_inspect`・`repository_read` を追加した。Run開始・Context編集・shellは拒否する。モデルへのHTTP送信より前の拒否であり、失敗した要求を完了した推論として扱わない。

## 更新候補と反映結果

ローカルcommit `f945f7abbbabc70815219d61f2ea503368cb77b0` から、`corepack pnpm run dist:desktop` と `corepack pnpm run dist:server` を実行して成功した。既存のRust実装を含むWindows release buildとNSIS作成、Linuxサーバーimage・archive作成を完了。外部へのpush・PR・配備は行っていない。

- Windows: `artifacts/desktop-0.1.0-preview.1-f945f7ab/koujou-san_0.1.0-preview.1_x64-setup.exe`。SHA-256: `3e9eea513d6a4a804f4c920ed5b542c5666bd0e87575fcf84e311bae4a4a026b`。
- サーバー: `artifacts/server-0.1.0-preview.1-f945f7ab/koujou-san-server-linux-amd64-0.1.0-preview.1-f945f7ab.tar.gz`。SHA-256: `94c4586a647d759e620d64272eb14be9776fa0d9e9e229547777f7c87df2e98f`。既存schema 28を維持。

接続先は `https://koujou.kingyosun.com`。初回観測時のアプリには未送信入力があり、サーバーの一時停止とアプリ終了を伴うため更新判断を求め、追加回答で承認された。更新前にはアプリとRunnerの終了状態を確認した。更新候補の作成とNAHTLOKAの受入確認を分けて記録する。

### 最終反映

- 共有サーバー: `45a1a94d73e873eba279b6872350cea23ab0c1b8`。適用image IDは `sha256:a585f9485cf0e73a8beb5a7a9c4ccaf00d6b04dc974e6f7fea81e6315732e72d`。
- Windows: `63aa8492b8acbfe5145fa2d0924aabc29e020ba8`。インストーラー終了コード0。インストールされたexeのSHA-256は `22168f90fa48040de0aa3d9f88b243b2e4108aa1c2b8b25965bcfca6cab23912`。TauriのNSIS bundle markerの差分だけを反映したbuild成果物と完全一致する。
- サーバーとWindowsの共通UI assetsは全ファイルで一致。サーバーのcodeはWindows候補の祖先commitと同じ。
- 更新前のprivate backupを保持した。最終backupは `/home/kingyosun/koujou-local2/private/update-45a1a94d/before-update.tgz`。schema 28、既存設定、本人・Workspace・所属、Context・Run・チャット履歴の保持と7サービスのhealthyを確認した。Gitea・Immichは再起動していない。
- Docker DesktopのOCI index IDと配備先Dockerのconfig IDの差は、archive checksum、index→manifest→configのhash連鎖、全layerとcommitの一致で確認した。元metadataを保持し、配備先bundleのimage IDだけを同一imageのconfig IDへ正規化した。配備手順とこの照合は独立検品済み。

最終追加検証は共通UI162件成功・専用セットアップ1件skip、Windows native34件成功・専用実接続12件ignore、`check:desktop`成功。公開Hostとloopback転送の試験・実MCP統合試験も成功し、Host修正とネイティブ読取り許可の追加はそれぞれ固定成果物の独立検品でblockerなし。

### NAHTLOKAでの受入結果

更新したWindowsアプリで、既存認証とWorkspaceを保持してProjectチャットを開き、Project・ChatGPT `gpt-6.1-sol` が自動選択されることを確認した。MCPの再認証は製品の「MCPへ接続」から完了した。

「開発の準備を確認する」→「送信」から相談を作成。最初の実相談は `project_inspect` 4回、`repository_read` 3回で設定と固定commit `8f7d9f6071f35e8a0d2e74daca728f66493e6086` の一覧・設計書を取得し、completedになった。登録先には設計書2ファイルだけがあり、README・実装コード・テスト基盤は未登録。Workspaceの実行許可はfalse、RunnerとRunの取得ページは空だった。

続けて「上の確認結果を踏まえて、koujou-san側で不足している設定だけを3点以内で整理」と送信し、前の人間・AI発言と今回の発言の3つの固定参照が送信されたことを保存記録で照合した。2回目もcompletedとなり、入力欄は「履歴4件」に更新された。設定・Runの開始は行わず、不足設定の参照にとどめた。

実画面は `.cache/chat-preview/live-nahtloka.png`、受入の操作・道具回数・参照roleの照合は `.cache/local2/chat-live-summary.json` に保存した。失敗した検証操作は画面の終了操作で閉じ、診断履歴を保持した。新しい相談の返答を表示したままユーザーへ返す。

### PRとCI

追加依頼に従い、改修ブランチをpushして[PR #101](https://github.com/KingYoSun/koujou-san/pull/101)を作成した。CIとマージの最終結果は同PRを参照する。

初回CIの `test:forge` は、セットアップ試験が旧UIのProject作成リンクの常時表示と手動のスレッド作成を前提にしていたため失敗した。試験をProject切替からの作成、最初の発言の保存による相談作成、モデルの自動選択へ更新した。実APIからスレッドと発言を再取得して保存内容を照合し、専用PostgreSQL・管理下のOAuth/HTTPによる `corepack pnpm run test:forge` が成功した。製品コードや認可条件の変更はない。

参照資料: [Slackのスレッド](https://slack.com/help/articles/115000769927-Use-threads-to-organize-discussions)、[サイドバー](https://slack.com/help/articles/212596808-Adjust-your-sidebar-preferences)。Slackのチャンネル構造は採用せず、相談ごとの画面を維持する判断に更新した。

実装資料: [react-markdown公式](https://github.com/remarkjs/react-markdown)、[GiteaのGit treeページ取得](https://docs.gitea.com/enterprise/api/21/operations/get-tree/)。
