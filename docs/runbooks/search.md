# Bleve検索の確認と復旧

検索はGo API内の専用Bleveコンポーネントが担当する。本文・版・直接所属・権限の正本はPostgreSQLにあり、APIが業務更新をBleveへ直接二重書き込みする構成ではない。検索の停止や反映待ちは、完全な0件として返さない。

## 起動と保存場所

通常の`pnpm run dev:api`で起動する。開発コマンドは`KOUJOU_SEARCH_DIR`の相対パスをリポジトリrootから解決し、未指定なら`.cache/search`を使う。配備時は書き込み可能な専用ディレクトリを指定する。索引は一つのAPIプロセスが所有し、複数プロセスから同じ保存場所を同時に開かない。

初回、保存位置に必要な履歴がない場合、schema/Analyzerの版が異なる場合は、PostgreSQLから別世代へ再構築する。索引を利用できるまでは検索APIが利用不可を返す。APIのreadyz成功だけで検索の準備完了とは判断せず、検索APIの完全性と反映位置を確認する。

## 検索API

`POST /search/workspaces/{workspace}/query`に、`text`、`kinds`、`ids`、`revisions`、`scopes`、`tags`、`limit`、`cursor`を渡す。limitは1〜100。省略した本文条件は全候補、版未指定は現在版、指定版はその不変版を対象にする。複数の種別・ID・版はそれぞれOR、Scopeとタグは指定したものすべてへ一致する条件とする。Scopeは直接所属であり、文書の子へ継承しない。

本文の検索は日本語・英語をCJK Analyzerで解析し、検索語のtokenをすべて満たす候補を返す。ID条件は完全一致。コード識別子は本文の語として検索できる。候補はスコアとIDで順序を確定する。`count`はそのページで返した許可済み候補数で、内部の生のhit数を公開しない。

返却する不変版と、その本文を検索するために使った固定参照先を、現在のSoT権限で確認する。cursorはUser・Workspace・条件・索引世代・反映位置へ結び付け、内部のsort keyを暗号化する。別ユーザーへ転用できず、条件・世代・位置が変わった場合は最初から検索する。

| 完全性・理由 | 扱い |
| --- | --- |
| complete | 指定した検索範囲をこの反映位置で取得した |
| partial / limit_reached | 続きがある。next_cursorで取得するか範囲を絞る |
| pending / projection_pending | PostgreSQLの位置へ反映が届いていない |
| pending / required_version_pending | 指定した既存版が索引に存在しない |
| pending / search_unavailable | 索引を利用できない。PostgreSQL検索へ代替しない |
| pending / invalid_cursor | 条件・User・世代・反映位置を再確認し、最初から取得する |

## Context入力の確定

`POST /search/workspaces/{workspace}/resolve`は、`search`と既存のContext解決要求`resolve`を受け取る。サーバーで検索を実行し直し、範囲・完全性と、明示選択したinclude/excludeが返却候補に含まれることを照合する。候補を順位だけで自動注入しない。

mutationを省略するとプレビューになる。`id`と`mutation`を指定すると、既存の認可・CAS・不変Manifest保存を呼ぶ。停止・反映待ち・部分結果・必要版の未反映ではManifestを保存しない。既存の明示参照用`/contexts` APIと、確定済みManifestは変更しない。

## 再構築と障害からの復旧

再構築は別の世代へsnapshotを作り、その後の差分も取り込む。正本と件数・版を検証してからactive世代を切り替える。切替前に失敗した場合は既存世代を維持する。再構築中に発生した新しい更新も、反映位置で識別する。

明示的な再構築は、APIを一度停止し、`KOUJOU_SEARCH_REBUILD=1`でAPIを起動する。既存世代を開いてからバックグラウンドで再構築する。完了を検索APIで確認した後、次回は既定の0へ戻す。DBや業務データを削除する必要はない。索引ファイルの手動編集や、起動中の別プロセスによる再構築は行わない。

## 検証と測定

`pnpm run test:search`は分離した実PGと永続Bleveを使い、品質・認可・版・所属・削除・再送・再起動・再構築・Context入力確定をHTTPから照合する。

`pnpm run test:search-load`は合意したWindows環境で、1万Context×3版・100 Scopeと7500件の所属版を再生成する。正解40検索をwarmupとして実行し、4クライアントがそれぞれ40検索を行う。HTTP/JSONと現在のSoT認可を含み、Oryの外部トークン検証の時間は含めない。再構築時間、反映遅延、p95、プロセス最大RSS、プロセスI/O転送量を`test-results/search-load.json`へ出す。I/Oはファイルとネットワークを含むプロセス値であり、PostgreSQLやOS全体のディスクI/Oとは区別する。

目標と測定環境は[計画](../progress/2026-10-15-issue-9-plan.md)を参照する。別のOS・runnerの機能試験を、このWindows端末での性能達成と数えない。
