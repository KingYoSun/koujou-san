# 全機能の導線監査

## 対象と終了条件

本人の依頼は、Project・相談・リポジトリに限らず、すべての機能について導線のない箇所を洗い出すことである。基準は`7ac07c5ca529f101a33863b6cd9a507e393cc404`。開始時のcheckoutはclean。個別修正は未着手であり、今回は監査と報告に範囲を閉じる。

対象は、現行の主ナビ20画面、認証・招待・popover等の主ナビ以外の入口、それらが呼ぶ業務API/MCPとネイティブ操作である。設計書の将来案を実装済み機能へ混ぜず、APIの内部処理をそのまま一つの利用者機能とは数えない。各画面の作成・取得・編集・保存・実行・停止・再開・復旧・関連画面への操作を対応付ける。

| 条件 | 対象と期待結果 | 判定方法 |
| --- | --- | --- |
| AC-A1 | 20画面と画面外の入口を漏れなく列挙する | navigation、Workspace shell、公開認証入口、UI controlと呼出し先の機械抽出を照合 |
| AC-A2 | 利用者向けの業務操作をAPI/MCP/nativeと照合し、UI入口あり・内部処理・運用者専用・未提供を分類する | 登録handler、invoke、UI handler、手順書と現行コードの参照を記録 |
| AC-A3 | 各機能の前提を、新規Workspace、既存データあり、Web/native、管理者/編集者/閲覧者の表示条件で調べる | 作成入口、ガード条件、折りたたみ、対象選択、権限・実行能力の依存を追う |
| AC-A4 | 導線欠落は再現条件・期待と実際・根拠・影響・正常な代替経路の有無で報告する | 初期データがないため作成にも進めない循環、必要ID等の取得元、画面間リンク、文書の案内も照合 |
| AC-A5 | 監査の網羅範囲と未確認事項を明示し、個別修正と区別する | 全項目の判定を記載し、実操作していない箇所を操作済みとしない。製品/本番データの変更なしを照合 |

外部への実書込み、実LLM消費、ユーザーのWorkspaceやrepoの作成・設定変更はしない。個人環境の情報をGitへ収録せず、本人への手動テスト依頼は行わない。以前拒否されたブラウザoriginを別経路で観測しない。

## 判定の区別

- 導線欠落: 通常の利用者操作を始める入口がない。
- 条件による行き止まり: 操作部品はあるが、その表示前提を同じ初期状態から満たせない。
- 運用経路のみ: CLI・設定ファイル等の正規経路はあるが、アプリ内の一般利用者向け入口はない。
- 意図した制限: 閲覧者やWebに実行能力がない等の既存契約。導線欠落には数えない。
- 入口あり: 具体的な画面・操作と前提を確認したもの。機能全体の実接続成功を意味しない。

## 結果

導線監査を完了した。主ナビ20画面をすべて確認し、画面外の認証・招待・popover、業務API、MCP 30 tool、nativeの登録39 commandを照合した。**13件の指摘**を下表に記録する。修正・本番データの操作は行っていない。

確認方法は、固定commitのコード、表示条件・handler・呼出し先、現行手順書の照合である。実画面を全操作した機能試験ではない。UIの構文解析では76ファイルから操作・入力・開閉等805定義を抽出した。805は機能数でも画面に同時表示される部品数でもない。内訳はbutton 361、form 8、anchor 17、画面リンク42、Disclosure 51等であり、動的なlabelやtrue/falseの分岐はソースを読み直して判定した。

### 指摘一覧

P1は初回利用・相談の開始/継続を阻むもの、P2は正規の準備・管理・関連対象への到達が不足するものとする。運用者専用や内部対応のみの項目は、一般ユーザーに追加権限を与える根拠にはしない。

| ID | 優先 | 機能・分類 | 実際の状態と影響 | 正規の別経路・根拠 |
| --- | --- | --- | --- | --- |
| N01 | P1 | Project/相談開始・文脈内の入口欠落 | 空のProjectチャットはProject再取得と空表示だけ。Project作成/作成画面へのリンクがなく、相談作成フォームもProject選択後にしか現れない。最初のProjectを用意できないように見える | Project自体はContextのScopeとして作成できる。`project-chat.tsx:395,441,492,747`、`context-explorer.tsx:410` |
| N02 | P1 | Workspace/Projectのrepo設定・機能未提供 | Workspace作成は名前のみ、Project Scopeはkind/name/parentのみ。WorkspaceまたはProjectにrepoを登録・選択する設定はない。Runごとの論理repo/commitと、端末のpath登録で代用しており、初期のプロジェクト設定にはならない | `team.tsx:470`、`contexts/model.go:56`、`runner-settings.tsx:117`、`runs/assignment.go:49`。Workspace一つ一repoという仕様があるとも判断しない |
| N03 | P1 | Gitea repo新規作成・製品入口未提供 | repo作成を始めるUI・専用API・Projectチャットtoolはない。相談からGiteaに新規repoを作る初期手順がつながっていない | Gitea自身の画面、または準備済みRunの明示CLI/API操作が別経路。観測/起票は既存repoを前提。`gitea/client.go:146`、ADR0034、`client-selection-host.ts:278` |
| N04 | P1 | ProjectチャットのContext参照・準備への入口欠落 | Context参照は初期値trueだが、同画面にはMCP接続/状態/接続設定への案内がない。新しいWeb/nativeクライアントでMCP認証がないと、通常ログインやLLM設定を済ませても相談が失敗する | 「Context編集→入力選択→MCPへ接続」はあるがチャットからリンクされない。`project-chat.tsx:87,705`、`auth.tsx:124`、`auth/mcp_client.go:38`、`client-selection.tsx:757` |
| N05 | P1 | Projectチャット操作の復旧・操作入口欠落 | 操作状態と原応答取得だけを表示し、failed/incomplete/readyの明示再開・担当引継ぎ・終了の操作がない。再訪やMCP接続後に元の相談を続ける経路がなく、新しい発言・要求を作り直すことになる | 同じclient-operationのAPIと共通Context選択画面にはtakeover/resume/cancel等がある。`project-chat.tsx:630`、`client-selection.tsx:821`、`clientops/operations.go:249` |
| N06 | P2 | GitHub/Giteaのrepo/観測登録・運用経路のみ | 観測画面は取得・履歴・出典表示のみ。新Workspaceへinstance/repo/target/bindingを設定する画面や、担当者へ接続設定を引き継ぐ入口がない | 運用者の`KOUJOU_GITEA_CONFIG`等。新Workspaceへ自動で既存の許可を与えないのは正常。`github-observations.tsx:303`、`githubobserve/service.go:187`、`runbooks/gitea.md` |
| N07 | P2 | 外部起票の接続選択・必須入力の取得元欠落 | 下書きの必須「起票接続ID」は自由入力のみ。接続候補・repoの選択、使用できるIDの一覧、設定への移動がない。空Workspaceの利用者は必要な値を画面から取得できない | 運用者のpublication設定ファイルを知れば入力できる。publication APIも接続一覧を公開していない。`publications.tsx:686`、`publications/http.go:169`、`runbooks/gitea.md` |
| N08 | P2 | 明示的な関係の登録/編集・APIのみ | 関係の探索・利用先は表示できるが、任意のreference/uses/generated/semantic関係を登録・変更する画面操作がない | `PUT /graph/.../relations/{id}`と`PutRelation`はある。文書のchildren/Scope所属/自動生成由来は別経路であり、任意関係の編集の代替ではない。`graphindex/http.go:22`、`relations.go:57`、`context-explorer.tsx:464` |
| N09 | P2 | デスクトップAPI接続の確認/変更・初回後に入口消失 | 接続保存後は初回フォームを描画しない。「設定→API接続」は接続状態だけで、URLの表示も変更フォームもない。サーバー移設や誤った接続先をアプリ内で変更できない | native configure自体もAuth初期化後は拒否するため、単にボタンを出すだけでは解決しない。`desktop-connection.tsx:14`、`App.tsx:25`、`connection.rs:50` |
| N10 | P2 | Projectを探す・検索/絞り込み入口不足 | 全Scopeのcatalog pageを取得してからブラウザでProjectに絞る。先頭ページが他種Scopeだけでも「Projectはありません」となる。Project名検索や種別絞り込みがなく、多数のScopeのページを順に辿る必要がある | 次ページボタンはあるため完全な到達不能ではない。catalogは100件単位で、scope_kindは返却項目であり検索引数ではない。`project-chat.tsx:166-175`の`listProjects`、`contexts/catalog.go:43,77` |
| N11 | P2 | 個別資源の共有/権限管理・内部対応のみ | Workspace参加者の役割はUIで管理できるが、Project/Context等の個別restricted・read/update/execute grantを編集する利用者入口はない | 判定台帳と`Access.PutResource`はあるが、公開access操作にも資源grant編集はない。`access/service.go:254,288`、`access/http.go:257`。グルーピングが権限付与にならない契約は維持する |
| N12 | P2 | 根拠から設定・編集へ戻る・対象引継ぎなし | 利用量詳細の「この根拠からモデル接続/Context/工程定義の編集へ」はscreenだけを渡す。該当接続・固定Context・定義を指定しないため、単なるカテゴリ移動になり、利用者が再探索する必要がある | 各機能の通常メニューは存在する。全リンクが壊れているという意味ではない。`usage-record-details.tsx:90-109` |
| N13 | P2 | 人手Issue情報の初回作成・作成操作が読み取り名に隠れる | Issue ScopeのID指定・優先度取得後、「Issue情報を取得」を押すと既存がなくても新規入力が現れ、version 0で保存できる。機能はあるが「新規Issue情報の作成」として発見できず、Scope作成への案内もない | 正規の新規保存は可能で、取得失敗で永久に進めないという指摘にはしない。`automation.tsx:1334,1730`、`triage-panels.tsx:238-279,397` |

### 20画面の機能照合

下表は作成・更新・参照・実行/停止・復旧の主要操作群をまとめた全画面の確認結果である。指摘のない行も入口と前提を調べた。詳細なボタン単位の機械抽出結果は作業用inventoryへ保存している。

| 画面 | 確認した操作・入口 | 初期状態/権限/platformの判断 | 指摘 |
| --- | --- | --- | --- |
| 作業と判断 | Run新規準備、採用定義から準備、手動入力、入力/Contextプレビュー、作成・割当・開始・取消、Pause回答・回付・差し戻し、関連Run/結果、利用量・export | 空でも「新しいRunを準備」がある。実行は別の実行許可・登録端末/接続が必要。native実行記録照合だけをWebへ開放しない | N02。その他入口あり。`workbench.tsx:606`、`runs.tsx:511,902`、`run-pause.tsx:384` |
| Projectチャット | Project選択、スレッド作成、発言保存/AI相談、履歴選択・取得、仕様採用、Run定義準備、関連Issue/Run/工程、popover | Projectなしで作成案内なし。スレッド作成はProject選択後、編集者以上。Context参照の準備と復旧操作が不足 | N01/04/05/10。`project-chat.tsx`、`chat-work.tsx` |
| Issue優先度 | 一覧、ScopeをID指定、Issue情報編集/新規保存、評価・根拠・入力取得、優先度手動固定/解除、外部同期確認 | 初回Issue Scopeを作る前提。情報作成は「取得」から空の新規値を開く。閲覧者の編集不可は正常 | N13。`automation.tsx:1334`、`triage-panels.tsx:221` |
| 外部起票 | 空の新規下書き、固定Run出力から下書き、保存、preview、送信受付、再照合、履歴、出典 | 下書き作成は空でも可能。外部送信は明示操作・現在許可が必要。接続IDの取得元が不足 | N06/07。`publications.tsx:617,744,768,787,829` |
| Context編集 | 文/段落/節/文書の新規作成、編集・子の組立て・順序、参照・タグ、Scope新規/親/所属、検索・グラフ・版履歴・下書き、手動/LLM選択・Manifest、元出力からの生成/採用/自動方針 | ScopeはContext本文なしでも作れる。同期確認済みと編集権限が作成の前提。MCP接続ボタン、失敗/質問/提案/引継ぎ操作はここにある | N01との接続不足、N08。`context-workspace.tsx:298,335`、`context-explorer.tsx:274,410`、`client-selection.tsx:726,757,821` |
| Run・Workflow定義 | 種類選択、新規下書き、JSONファイルimport/lossless編集、AI生成、端末能力取得、静的検証・検証版保存・採用、Run準備、工程ノード/依存/入力/差し戻し編集、工程開始/判断/入力preview/採用版影響/適用、export | 空から作成可。未採用版の実行を抑止するのは正常。定義のimportもWebで可能、exportはWeb/nativeの両分岐あり | N02のrepo前提。その他入口あり。`workflows.tsx:552,583,846,899,925`、`workflow-execution.tsx:344,558,648,704` |
| 検品定義・証拠管理 | 新規定義/証拠、JSON編集保存、採用、端末/repo指定と検品Run依頼、履歴/判定、別判定比較、人の判断保存 | 空から作成可。検品開始は採用・実行許可・端末を必要とし、disabled理由あり。具体的な基準/証拠編集はJSON方式 | N02のrepo設定への接続。その他入口あり。`inspections.tsx:530,533,576,610,638,758` |
| Hopper | 初期templateで新規ルール、意味判断・モデル取得、条件/JSON保存、有効/無効、発行なし試行、評価/試行/原入力・原応答、再評価、発行/割当再開、連鎖停止/再開 | 新規作成あり。有効化・試行・発行等の管理者制限は正常。LLMは登録済みサーバー接続が必要 | サーバー設定の運用境界は下表。`hopper.tsx:576,610,733,777,899,964,1099,1124` |
| Triage方針・評価 | 新規template、段階追加/削除・基準・Scope・優先順、モデル/詳細JSON、有効/無効、一括評価範囲固定・継続・停止、要求/根拠/履歴 | 空から方針を作れる。Scope指定はID方式。既存LLM許可・起票同期接続が別途必要 | N06/07/13。`automation.tsx:1095,1128,1276,1318`、`triage-panels.tsx:560,602,608` |
| 共通の処理運用 | 種類別の主体・取得/確定許可・同時枠・試行・期限・予算設定、Worker一覧、履歴、停止/再開 | 管理設定は管理者のみ、初期値から保存可。ジョブ操作は既存行がある場合だけ出るのが正常 | 入口あり。`automation.tsx:1390,1514,907,923,946` |
| 費用・品質・利用量 | 期間集計、帰属/Scope/モデル絞込み、記録/版、遅着・訂正、元計測/料金取得、費用再評価、export、二群比較・条件保存・quality/時間/条件 | 空でも照会可能、値がない状態と未取得を区別。比較条件の保存/新規とWeb exportあり | N12。`usage-pricing.tsx:478,593,631`、`usage-comparison.tsx:206,343,370,377,397` |
| GitHub・Gitea観測 | 共有観測取得、対象選択、前後page、変更履歴、sourceを開く | Webはanchor、nativeはOSで開く正規分岐あり。repo/target/bindingの登録は画面外 | N06。`github-observations.tsx:186,249,338,426` |
| アカウント | ログイン/作成・確認・復旧、提供されたidentityフォーム、設定・ログアウト、追加認証 | Webには認証専用URLとリンク。nativeは外部認証画面の正規入口。未ログイン時もWorkspaceなしで始められる | 入口あり。`auth.tsx:400,463,498` |
| Workspace・参加者・招待 | Workspace新規、選択・ページ、招待作成/取消/受諾、メンバー役割/実行許可/有効状態、離脱 | Workspaceなしでも作成/招待受諾が可能。参加者変更の管理者制限は正常。repo/Projectの初期設定はない | N02、個別資源ACLはN11。`team.tsx:470,488,556,644,693` |
| 端末・共有ポリシー | native端末登録/更新・接続確認、admin端末許可/失効、LLM送信禁止tags/Scope | Webは端末OSの実行資格を作らず、管理者が共有許可を扱う。DLP方針保存あり | 個別資源ACLはN11。`team.tsx:733,742,827`、`delivery-policy.tsx:57,116` |
| CLI・独立Runner | native CLI program/引数/接続方式・入力・usage/WSL設定、保存、Runner起動/状態/受付停止、接続能力とrepo論理ID→端末pathの保存/再送 | nativeかつ編集者以上の機能。Webに任意CLI実行能力がないのは正常。repo作成・clone/Workspace既定設定とは別 | N02/03。`workspace-shell.tsx:291`、`execution-settings.tsx:74,213,250`、`runner-settings.tsx:33,117,130` |
| クライアントLLM接続 | 直結接続新規/保存、Webメモリkey、native提供元ログイン/切断/認証画面、server候補取得・選択・利用設定、料金参照 | Webでサーバー接続は選べる。native専用OAuthをWebで行わない既存契約は正常。モデル選択は各操作にもある | サーバー割当の初期案内不足は運用表参照。`model-connection-settings.tsx:192,221,408,426` |
| サーバーLLM接続 | 登録済み接続・版・モデルの再取得と一覧、Hopper/Triageへの移動 | 運用者ファイル/CLIで登録・認証。新Workspaceへ自動的に資格情報を共有しないのは正常。Webログイン撤回を未修正bugに数えない | 運用経路のみ。`server-model.tsx:49,84`、`serverllm/gateway.go:51` |
| 料金表・費用評価 | 料金新規/取得、モデル/契約/期間/区分/丸め/単価編集、料金版保存、根拠から費用再評価 | 編集者以上が新規作成可能。既存実モデル/接続ID/料金情報が入力前提。計測値を単価から作る機能ではない | N12。`usage-pricing.tsx:677,769,822`、`usage-reevaluation.tsx:137` |
| API接続 | 接続状態表示、nativeの初回URL設定 | Webの接続先は現在のサイト。nativeは保存後に初回URLフォームが消える | N09。`App.tsx:25`、`desktop-connection.tsx:14` |

### 画面外・内部・運用者の機能

| 操作群 | 入口の判断 | 根拠 |
| --- | --- | --- |
| Web認証・追加認証・メール確認・復旧 | 正規の専用URL・identityフォームに入口あり | `auth.tsx`、`auth/http.go` |
| 招待リンクから参加 | Workspaceをまだ持たなくてもpreview/acceptする入口あり | `team.tsx:379,488`、`access/http.go:244` |
| popover Projectチャット | チャット以外の画面の右下launcher、通常画面へ移動、同じスレッドを維持する入口あり。Projectなしの問題は共通 | `workspace-shell.tsx:326`、N01/04/05 |
| MCP接続とMCP認証状態 | 接続ボタンは共通Context選択にあり、チャットにはない。native `mcp_status`は内部状態APIであり、独立した利用者機能の欠落とは数えない | `client-selection.tsx:757`、native登録39 commandとの照合 |
| サーバーLLM登録/ログイン/モデル一覧/切断 | 運用者のコマンドと設定ファイル。Webの登録/ログインは本人が撤回済み。新Workspaceの許可追加については空画面から担当者・手順への案内が不足 | `chatgptplan/command.go`、`runbooks/server-models.md` |
| GitHub App/Gitea credential・観測/起票許可・Webhook設定 | 運用者側でのみ設定、一般利用者の画面入口はない。勝手に全Workspaceへ権限を拡張しない | N06/07、`runbooks/gitea.md`、`githubobserve/config.go`、`publications/remote.go:31` |
| サーバー初期化/管理者bootstrap/backup/restore/update/CA変更 | 正規の配布CLI・手順あり。Workspace作業者の通常UIにないことを単独のbugとは数えない | `cmd/koujou/main.go`、`runbooks/distribution.md` |
| 検索/探索の再構築・projection追随 | 検索/graphの通常UIは取得・状態表示。再構築は運用・内部機能として扱う | `searchindex`、`graphindex/service.go:193` |
| 外部CI/PR作成・再実行・status・merge・LFS | 観測と実行は別。操作は登録済みRun/CLI/APIへ明示依頼する契約で、チャットの読取りtoolへ直接追加されていない | ADR0034、`runbooks/gitea.md`、N03 |
| sync・模型通信・run report・lease等 | 単独のユーザーボタンではなく、保存/実行/認証の内部経路としてUI操作へ対応付く。内部APIの全てに専用画面を要求しない | Go handler/invoke/MCP inventory、`sync-engine.ts`、`client-operation-port.ts` |

### 意図した制限と、欠落から除外したもの

- 閲覧者に作成/編集・モデル消費を許可するボタンを出さないこと、管理者だけが有効化・共有権限を扱うこと。
- WebへOSのCLI/Runner実行・端末credentialを暗黙に与えないこと。
- 保存・採用・有効化・実行開始・外部送信をそれぞれの操作に分けること。
- 結果不明のモデル/起票要求を、画面再表示で自動再送しないこと。ただしN05のように明示の復旧手段もない状態とは区別する。
- Run・定義・工程記録のexportがnativeだけという機械抽出上の候補は除外。実際にはWebのdownload anchorがelse分岐にある。
- 外部sourceを開く操作がnativeだけという候補も除外。Web anchorがある。
- 「Issue情報の新規保存が完全にない」という候補は除外。queryが空でもloaded=trueになり、新規保存できる。N13は入口の命名・前提への指摘である。
- Workspace改名・削除、工程全体の手動停止等は、公開操作自体の有無を区別する。今回の『実装済み機能の導線欠落』の件数に、任意のCRUDを新たな必須機能として足さない。

## 監査の限界と検証

本監査は導線の静的・構造的な照合であり、全機能の実画面・実接続テストのPASS報告ではない。画面幅、focus、各外部サービスの実アカウント、全状態のブラウザ実操作は未検証である。外部repo作成・LLM消費・既存Workspaceへの書込みはしていない。固定したコードで入口の存在・前提・非表示条件を確定できる指摘を記載した。

`CodeGraph`で入口と呼出し先を先に調べ、TypeScript ASTで76ファイルの805定義・290呼出し・50 Screen/ScreenContent定義を抽出して照合した。Go ASTのHTTP登録式とMCP tool、nativeの39登録commandも確認した。HTTPのhelper closureとloopがあるため、抽出登録式の数をそのままHTTP機能数とは扱わない。診断用auth probe、型変換の`windows.Handle`等の偽陽性も利用者機能から除外した。

変更したのは本監査文書とGit管理外の抽出用ファイルだけで、製品コードの修正には着手していない。次の修正ではN01〜05の初期利用を一続きに定義し、Workspace/Project/repoの責務と、外部作成を誰の権限で行うかを先に確定する必要がある。運用者専用・内部対応の機能を、導線監査だけを理由に無条件で一般UIへ開放しない。
