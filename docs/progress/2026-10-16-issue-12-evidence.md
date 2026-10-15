# Issue #12 検証記録

対象は[実装計画](2026-10-16-issue-12-plan.md)。既存下書きの移行と、移行後に旧版アプリを混在させない方針はユーザー承認済み。局所検証・実操作の結果を記録する。独立検品・CI・マージ後の判定は[current-state](current-state.md)で管理する。

## 保存・同期

- Windows: SQLiteへContext単位で保存し、本文・下書き・同期メタデータをDPAPIで暗号化する。`scoped_dpapi_records_migrate_restart_page_replace_and_purge`は成功。205件の下書き、未確認の操作ID、競合、receipt、再起動、一件置換、API/User/Workspaceの分離、削除、不正な旧形式からの再試行を確認した。
- ブラウザ: IndexedDBの旧Workspace一件をレコードへ移す。移行と旧データの削除は同じトランザクション。1万件の移行、最大100件のページ、一件編集時にContext一件と小さな同期ヘッダーだけを書き込むこと、他の下書きの保持、保存失敗後の移行再試行が成功した。
- 同期: `watch`で明示した対象を最大100件ずつ確認する。変更がない本文は返さず、無関係なWorkspace履歴を走査しない。既存のsnapshot/changes APIは維持する。実PGの`test:sync`、共通UIの競合・切断・再送・権限失効の試験が成功した。

## 業務API

`test:contexts`はScope 101件のページ境界・固定版履歴・Workspace境界を含めて成功した。最初の範囲比較ではScope候補の件数試験に失敗し、SQLを専用の部分索引に合わせて修正した。migration 009を追加し、既存001〜008は変更していない。

`test:search`は候補の本文分離、複数の固定参照を正確に指定する範囲、既存40検索・権限・投影停止・復旧を含めて成功した。候補は名前と固定参照だけを返し、本文は開いた対象で取得する。

## Webの先行実操作

専用PG/Ory/Bleve/ArcadeDBに1万Context×3版、Scope 100件、共有文を二つの段落から使う文書を用意した。Chrome拡張経由でID検索、文の編集、明示保存、新版、旧版履歴を確認した。更新後の親文書の版は維持され、親の注入プレビューには固定していた旧版の原文が二か所とも残った。実入力と配置の表示を照合した。

先行画面は`.cache/issue12-web-first-save.png`。この確認後に追加した正確な固定参照フィルター、空選択表示、Scope索引の修正は、最終実操作で再確認する。

`test:mcp:ui`の先行実行は成功。画面確認終了後、実Bleve停止・専用ArcadeDB停止・Ory token失効の既存試験まで通過し、専用Compose環境を削除した。

## 条件との対応

| 条件 | 確認した結果 |
| --- | --- |
| AC-1a/1b | Webの文→段落→節→文書の新規保存、Windowsの文の新規保存と四粒度の編集、固定参照の再利用・順序・新版採用・履歴。過去の親は維持 |
| AC-2 | WebのProduct/Issueへの複数所属、所属文書からの参照の区別。Windowsの既存所属解除でも本文版・子の参照を保持 |
| AC-3 | none、空選択、親＋子の重複、共有文の全配置exclude、抜粋、取得状態・由来・添付、異なる版の矛盾をresolverと照合 |
| AC-4 | 実Bleve close、専用ArcadeDB stop、投影の一時停止、深さ一段の打切り、未取得添付、Resource権限失効を画面と照合 |
| AC-5 | 実操作後の正本の更新種別はContext関連だけ。要求ログにもRun/実行入口はない。保存されたManifest数も実操作では増加しない |
| AC-6 | 二UserのWeb/Tauri競合と収束、Webの通信停止・再接続・再読込後の下書き復元と明示再適用、失効後の削除を確認 |
| INVAR-1〜5 | Zustandの画面状態、TanStack Queryの共有表示、正本・下書きの分離。Scopeは自動注入せず、ハーネス・端末登録を編集の前提にしない。WebにCLI実行を追加しない |
| 規模 | 1万Context×3版・Scope 100件で本文を含まない候補ページと対象単位の本文・保存・watchを確認。Scopeの実行計画は専用索引100件＋主キー取得100回、実測0.467 ms |

## 証拠

- [Web/Tauriの競合](assets/issue12-web-native-conflict.png)、[抜粋と由来](assets/issue12-excerpt-origin.png)、[Windowsの所属解除](assets/issue12-native-grouping.jpg)、[探索からの参照選択](assets/issue12-graph-reference-preview.png)。
- [参照版の明示採用と順序](assets/issue12-adoption-order-proof.json)、[所属解除と不変の本文版](assets/issue12-native-grouping-proof.json)。
- [Scopeの実行計画](assets/issue12-catalog-explain.txt)、[実操作中の更新種別](assets/issue12-ui-mutation-kinds.txt)、[要求の集計](assets/issue12-request-summary.json)。2回の実操作でpullは429回/688回、すべてwatch、対象数は最大6件/1件だった。

## 最終の局所検証

`pnpm run check / test / build / check:desktop / test:desktop / build:desktop`、`test:database / test:sync / test:contexts / test:search`が成功した。Windowsの保存試験は3件成功し、対話環境が必要な既存試験5件は通常実行ではignore。Context実画面用の正規Ory認証とDPAPI資格情報準備は明示実行で成功した。

最終の`test:mcp:ui`は実操作後に新しい正規MCP tokenで接続し直し、実Bleve停止・専用ArcadeDB停止・Ory token失効まで成功した（Go試験393.513秒）。合成データ用gatewayの通常のrefreshも実行した。標準DB、既存ユーザーのパスワード、信頼済み証明書の設定は変更していない。

その後、保存失敗からの再試行前に新しい保存が上書きされるケースと、サインアウト後に遅れて届くディスク読込が復活するケースを失敗する試験で確認して修正した。共通UIは34試験成功。名前とタイトルを持つ既存文書も、編集するタイトルを正しく表示するようにした。旧キャッシュの実行報告もwatchで正しく照合し、実PGの同期試験で更新・変更なし・現在認可を確認した。

依存の一次資料: [rusqlite 0.40.2](https://docs.rs/rusqlite/0.40.2/rusqlite/)、[fake-indexeddb](https://www.npmjs.com/package/fake-indexeddb)。

## 追加の実操作

Webで文・段落・節・文書を新規作成し、固定した子を順に追加して保存した。文書から文までの四配置と実入力が一致した。ProductとIssueへ同じ文書を所属させても文書の版は変わらず、所属文書からの参照は直接所属と別の説明で表示された。

Windowsで文を新規保存し、Web作成の段落と節、検証文書の新版を保存した。参照する子の固定版は保持された。Web/Tauri間では同じ文の競合を検出し、基準版・共有版・下書きを比較して再適用後に収束した。接続を止めたWebで下書きを保持し、他Userの更新後に再接続・画面再読込みを行っても、競合下書きは自動送信されず、明示再適用後だけ共有版が変わった。

none、空選択、親と子の重複、共有文の全配置からのexclude、異なる版の矛盾、未取得添付を確認した。抜粋の入力には残した文と固定添付だけが入り、由来・配置・除外を表示した。実Bleveのclose、専用ArcadeDBコンテナの停止、投影の一時停止、深さ一段での探索により、停止・遅延・打切りの表示を確認した。

Resource権限の失効で本文と未送信下書きが消え、以前の入力プレビューも表示しなくなることを確認した。権限復旧後は共有版だけが戻り、削除した下書きは復元されなかった。

実操作中に見つかった新規下書きの消去、探索Nodeの表示情報を固定参照へ混入する問題、エラー時の理由の欠落、既存所属の探索方向を修正した。関連対象を開く際に編集中の基準版を保持する試験も追加した。各失敗を再現してから修正し、共通UIの関連試験が通過した。

実操作環境の長時間利用では、Goテストの既定10分と既存の短いaccess token期限にも達した。前者は実操作コマンドだけ35分へ延ばし、後者は専用gatewayで正規のrefreshを使うようにした。製品の認証期限や証明書検証は変更していない。実操作後のMCP試験も新しい正規tokenで再接続するようにした。修正前のtimeout・期限切れによる実行は成功に数えない。

Windowsの既存所属解除を修正後に再確認し、所属の前提版を更新しても表示中の文書の基準版は失われなかった。同期・Context・DBの実PG試験も最終差分で再実行して成功した。探索Nodeから選んだ参照の実入力プレビューは最終環境で成功した。
