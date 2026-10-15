# Issue #9の検証記録

- 対象: Issue #9、Scope revision 1、区分C。
- 基準: `a744639ee4aa7084e20a031fb114e4dd072c385b`。
- 実装の固定commit: `e0ab4c51cca41ca6dbc72f35997c1e03f2435837`。
- 現在の判定は[現在状態](current-state.md)へ集約する。

## 条件と観測

| 条件 | 実PG・実Bleve・HTTP APIの観測 |
| --- | --- |
| AC-1 | 四粒度の日本語10・英語10・コード識別子10・ID完全一致10の正解を、全て上位10件以内で取得。種別・ID・固定版・タグ、直接Scope所属を照合。Documentの子Sentenceへ所属を継承せず、39件の許可済みSentenceをページ継続で重複・欠落なく取得した |
| AC-2 | 2 Workspace、複数UserでID・本文・count・cursorを確認。制限付き子の本文を使う親候補も拒否し、権限外候補の件数・継続を公開しなかった。別Userのcursorを拒否。位置が変わらない対象権限取消とWorkspace失効も照合した |
| AC-3 | 作成・新版・所属解除・削除・再送・古いeventを照合。2更新をqueueし、test-onlyのIndex wrapperで2件目のbatchを停止。1件目とcheckpointだけを永続化して実際にclose/openし、pendingのまま復元。再開後は両版一致・文書件数＋2・completeとなり、再送で重複しなかった |
| AC-4 | 再構築先のmarker書込を実ファイル操作で失敗させ、旧世代と旧候補を維持した。別世代のsnapshot transactionをpg_stat_activityで観測した後に、本文更新と所属解除を実行。切替直後に本文の新版とScope候補0件・completeが一致した。保存位置の履歴を削除した場合もSoTから再構築した |
| AC-5 | 実検索とContext解決・保存を接続。利用可能な候補の明示選択だけがManifestを保存し、上限・反映遅延・要求版の欠落・索引停止では件数が増えなかった。索引の変更・再開後も以前のManifestの値は不変だった |
| AC-6 | 下記の合意負荷で全数値目標を達成。初回と途中の未達結果も残し、データや目標を緩和していない |
| INVAR-1〜4 | Bleve停止をPostgreSQL検索へ代替せず、業務更新は既存SoT経路へ保存。直接Scope所属と固定子参照を分け、明示選択を既存Context解決で再照合。順位からの自動注入や開始済みManifestの変更なし |

最終の`pnpm run test:search`は13.09秒で成功した。試験では実HTTP経路を使い、認証済み主体をfixtureで固定して認可を照合した。Oryそのものは別の実Ory回帰で確認した。本Issueは検索GUIを追加しておらず、画面確認の成功を新しい検索操作へ広げていない。

## 合意負荷と結果

[ユーザーが採用した目標](2026-10-15-issue-9-plan.md)を固定し、[保存した測定JSON](2026-10-15-issue-9-load.json)へ記録した。実装commit`7653b35`で測定し、その後の`e0ab4c5`は受入試験と開発proxyだけを追加した。検索のproductionコード・負荷fixture・測定条件は同じである。

| 項目 | 条件・目標 | 実測 |
| --- | --- | --- |
| データ | 1万Context×3版・Scope 100件 | 四粒度各2500件、3版ずつ、所属7500版も生成 |
| 品質 | 正解40検索で期待IDが上位10件以内 | 40/40 |
| 負荷 | 同時検索4 | warmup 40後、4クライアント×40＝160 HTTP検索 |
| p95 | 500 ms以下 | 23.5 ms |
| 反映遅延 | 5秒以下 | 1.013秒 |
| 再構築 | 120秒以下 | 48.877秒 |
| 最大メモリ | 検索プロセス1 GiB以下 | 145,309,696 byte（138.6 MiB） |
| I/O | 観測値を記録 | process read transfer 0、write transfer 78,922,531 byte |

Windows 11 Pro 10.0.26200、Ryzen 7 7800X3D（8コア16論理プロセッサ）、利用可能メモリ63.2 GiB、Go 1.27.2で実施した。標準の開発サービスを起動したまま、別の分離PGと永続Bleveを使った。HTTP/JSON・現在のSoT認可を含む検索業務APIの時間を測り、Oryの外部トークン検証は含めていない。最大RSSとI/OはWindows APIで当該Go試験プロセス自身を測った値。I/O転送量はネットワークやファイルを含み、read 0をOSの物理ディスク読取り0とは解釈しない。PostgreSQLやOS全体のI/Oとは区別する。

初回`1d6f657`は再構築278.879秒で未達だった。batch化後`af03800`でも195.441秒で未達。書込み転送量は2,307,665,591から78,922,687 byteへ減ったが、目標達成とはしなかった。

同じデータでgeneric planを調べると、所属の取得1回で7500行を起点に22,491回のjoin除外を行い、約20 msを要していた。対象IDの索引から候補の所属を求め、LATERALで各最新版を取得するSQLへ変更し、同条件で約0.65 msとなった。最終の再構築は48.877秒で成功した。途中のJSONとquery planはGit管理外の`.cache/issue-9/`にも保持した。

## 失敗と修正

検索の初回ページ継続は、39件を期待した試験が3件で終了して失敗した。BleveのGo内部sort keyはスコアを`_score`というmarkerで表し、SearchAfterは実スコアを要求するため、cursorへ数値を格納するよう修正した。同じ試験で全39件が一致した。

初回の統合fixtureは、Manifest保存の更新eventを反映する前に件数上限を調べ、projection_pendingとなった。反映を待ってから各理由を独立に観測するようfixtureを修正した。追加の中断fixtureでは、埋め込みinterface名とIndexメソッドが衝突するコンパイルエラーを修正し、最終試験は成功した。

独立検品では、同期完了後のclose/openだけでは反映途中停止を観測したことにならない点と、再構築中の所属変更が未観測である点が保留された。上記AC-3/4の実操作を追加して再検品し、全条件PASSとなった。製品用の試験hookや新しい要件は追加していない。

## 回帰と標準環境

`pnpm run check / test / build`、`test:database / test:auth / test:access / test:contexts / test:sync`は成功した。通常Go試験と共通UI・契約5ファイル24件が成功し、既存の保存・停止復元・Ory・現在認可・Context解決・共有同期を維持した。最後のproxyと追加fixtureの変更後にも`check`と実`test:search`が成功した。

標準PGへ追加migration006を適用し、APIを8080で再起動した。readyzが成功し、実Bleveのactive世代を生成した。直接8080・Web proxy1420の未認証POSTは同じ403で拒否した。既存のOAuth・Device資格情報は変更していない。Tauri実装は変更しておらず、本Issueのローカルdesktopコマンドは再実行していない。

CodeGraphを先に使い、当初未索引だったsearchindexだけ実ファイルへ切り替えた。後の固定commitでは索引からも確認できた。新しいCodeGraph索引の作成は行っていない。

仕様は[ADR 0008](../adr/0008-bleve-search.md)、操作は[検索手順](../runbooks/search.md)、固定commitの判定は[独立検品](2026-10-15-issue-9-inspection.md)を参照する。CIとマージ後照合は別に追跡する。
