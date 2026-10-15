# 0009: ArcadeDBによる関係の投影と探索

- 状態: Proposed（合意負荷と区分C検品の完了待ち）
- 日付: 2026-10-15
- 関連: Issue #10、設計書§6.6〜6.8・§8.3〜8.4・§8.7・§18、[実装計画](../progress/2026-10-15-issue-10-plan.md)

## 接続と正本

ArcadeDB 26.10.1の独立サービスへ、公式Go HTTPドライバ0.2.0とパラメータ付きSQLで接続する。SDKはGo APIプロセスが所有し、共有状態はPostgreSQLだけが確定する。本文・不変版・直接所属・採用した関係と由来を正本へ保存し、更新記録から投影する。再構築時にLLMを呼ばない。

一つの専用物理DBを単一の投影writerが所有する。開発用の既定名は`koujou_graph`。Workspaceと世代をすべてのノード・関係へ付け、APIではSoTの現在権限を照合する。複数writerやHAは今回の対象外である。

## 型と版

ノードにはContext四粒度、Scope、宣言済みRunを使い、安定IDのidentityと固定版のrevisionを分ける。構成は固定版同士の`contains`と配置順序で表す。`membership`はContextとScopeのidentity間の直接所属であり、構成やScopeの親子から所属や権限を継承しない。`version`は固定版からidentityへ結ぶ。

`reference / uses / generated / semantic`の正本は、新しい`graph_relations`の不変版である。`uses / generated`の起点は宣言済みRunに限る。このIssueでは実RunやCLIを実行しない。採用時のOriginを保存し、探索では関係の固定Sourceと正本のOriginを返す。

ArcadeDBのnative edgeを使う。内部RIDは接続に使うだけで、外部参照やcursorへ出さない。既定のidentityからの版展開は現在版に限り、`history=true`で過去版も辿れる。構成が明示する古い固定版は、その版のまま辿る。履歴の保存をArcadeDBのMVCCで代替しない。

## 反映と再構築

関係・ノード・反映位置を同じArcadeDB transactionで確定する。版を含む安定keyで重複を除き、期待する直前位置が異なる更新は反映しない。commit応答を失った場合は、実DBの位置を照合する。照合できないまま成功を返さない。

再構築はPostgreSQLのrepeatable-read snapshotから別世代へロードし、複数commitに分ける。snapshotのノード・関係数を照合し、snapshot後の本文・所属・関係の差分を取り込んでから、active世代を一つのtransactionで切り替える。中断した世代へ切り替えない。保持された更新履歴が欠けている場合もsnapshotから回復する。

## 探索と入力確定

探索はGo側でfrontierを束ねた幅優先探索とし、索引付きSQLで接続する関係だけを取得する。クライアントへ全グラフを転送しない。現在のWorkspace・ノード両端・関係所有者の権限を確認し、固定版と由来をSoTへ照合してから返す。権限外の関係は件数や継続の判断へ含めない。

ユーザーが採用した既定上限は深さ8、2千ノード、1万関係、1ページ100件、2秒。設定で変更できる。上限・timeout・truncated・stream切断・反映待ち・探索中の位置変更を区別する。readは完了Statsとreturned件数を照合し、行を受け取っただけでは成功としない。

cursorはUser・Workspace・条件・世代・位置に結び付け、暗号化する。安定した幅優先の発見順でページを返し、次の1件を確認したところで処理を区切る。継続時は必要な先頭範囲だけを再探索し、経路の現在の権限を確認する。検索と共有する暗号化処理は、従来のBleve cursorの鍵導出・形式を維持して抽出した。

`/graph/.../resolve`は探索を実行し直し、完全性と明示選択した固定Context版を照合した後で、既存のContext解決・Manifest保存を呼ぶ。探索候補を自動注入しない。不完全な探索やArcadeDB停止では新しいManifestを保存せず、保存済みManifestを変更しない。

## 永続化と復旧

既存の1 GiBコンテナ上限内で、heap・page cache・WAL flushを固定して評価する。測定環境と達成可否は[検証記録](../progress/2026-10-15-issue-10-evidence.md)へ置く。checkpointだけで復元の同一性を推定せず、ID・固定版・関係・位置を照合する。正本のbackupを優先し、ArcadeDBのnative backupは派生データの復旧補助として扱う。

根拠: [Goドライバ0.2.0](https://github.com/ArcadeData/arcadedb-drivers/tree/go/arcadedb/v0.2.0/go/arcadedb)、[SQL DELETE](https://docs.arcadedb.com/arcadedb/reference/sql/sql-delete)、[性能設定](https://docs.arcadedb.com/arcadedb/how-to/operations/performance-tuning)、[backup](https://docs.arcadedb.com/arcadedb/how-to/operations/backup)、[restore](https://docs.arcadedb.com/arcadedb/how-to/operations/restore)。将来版の文書にある機能は、26.10.1での実接続を確認してから採用する。
