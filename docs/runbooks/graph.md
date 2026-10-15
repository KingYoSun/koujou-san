# 関係探索の確認と復旧

関係探索は独立したArcadeDBへ接続する。本文・不変版・所属・採用した関係の正本はPostgreSQLにある。ArcadeDBを停止しても確定済みManifestは変わらないが、探索を必要とする新しい入力確定は利用できない。

## 起動と接続

標準ArcadeDBを起動してから`pnpm run dev:api`を使う。開発コマンドは既存のローカル資格情報から接続設定を渡す。別のプロジェクトの設定や認証情報はコピーしない。

| 設定 | 開発時の既定値・用途 |
| --- | --- |
| KOUJOU_GRAPH_URL | `http://127.0.0.1:2480` |
| KOUJOU_GRAPH_USER | `root` |
| KOUJOU_GRAPH_PASSWORD | ローカルで生成したArcadeDBの資格情報。配備時は専用の資格情報を設定する |
| KOUJOU_GRAPH_DATABASE | `koujou_graph`。派生データ用の専用DB |
| KOUJOU_GRAPH_DEPTH / NODES / EDGES / PAGE | `8 / 2000 / 10000 / 100` |
| KOUJOU_GRAPH_TIMEOUT_MS | `2000` |

一つのGo APIだけが同じ物理DBへ投影する。接続先を設定しない配備では、探索APIは利用不可を返す。Go APIの`readyz`成功は、グラフの反映完了を意味しない。

## API

`POST /graph/workspaces/{workspace}/query`へ次の形を渡す。rootのrevisionを空にするとidentity、固定版を渡すとそのrevisionを起点にする。`head`は受け付けない。

```json
{
  "root": {"kind": "sentence", "id": "shared", "revision": "fixed-revision"},
  "direction": "in",
  "kinds": ["contains", "reference", "uses", "generated"],
  "depth": 8,
  "limit": 100
}
```

directionは`in / out / both`で、省略時はboth。kindsを省略すると全種類を辿る。identityからのversion展開は既定で現在版に限り、`history: true`で過去版も含める。固定構成が明示する過去版はそのまま辿る。本文を読むには既存のContext APIを使う。

応答のitemsは、型・安定ID・版を持つnodeか、両端の固定参照・種別・順序・Source・Originを持つedgeである。native RIDや、権限外のID・関係数は返さない。cursorはUser・Workspace・条件・世代・位置に結び付く。条件・世代・位置が変わった場合は最初から取得する。

| 完全性・理由 | 対応 |
| --- | --- |
| complete | 指定した範囲の探索がこの位置で完了した |
| partial / page_limit | next_cursorで次のページを取得する |
| partial / depth_limit・node_limit・edge_limit | 未探索範囲が残る。探索条件を絞る |
| partial / timeout・truncated・stream_incomplete | 必要な結果を得ていない。復旧後に再取得する |
| partial / projection_changed | 探索中に世代や反映位置が変わった。再取得する |
| pending / projection_pending | 正本の位置への反映を待つ |
| pending / required_version_pending | 必要な固定版や由来が投影または正本で確認できない |
| pending / graph_unavailable | 接続・投影・schemaを確認する |

`unexplored`には同時に生じた中断理由を並べる。ページの上限だけを見て、他の未探索理由を見落とさない。完全性と版・位置はContext解決にも渡す。

`PUT /graph/workspaces/{workspace}/relations/{id}`は、operation_id・expected_versionとvalueを受け取る。valueはkind・固定版のfrom/to・order・present・任意のorigin。書込み権限と両端の現在の読取り権限が必要で、`uses / generated`の起点は既に宣言されたRunだけである。端末へ実行を依頼するAPIではない。

`POST /graph/workspaces/{workspace}/resolve`は、`graph`と既存のContext解決要求`resolve`を受け取る。サーバーで探索を実行し直し、完全性と明示選択した固定Context版を確認する。mutationなしはプレビュー、idとmutationを指定すると既存のCAS・不変Manifest保存を呼ぶ。不完全な探索を使って保存しない。候補を自動で入力へ注入しない。

## 再構築と永続化

初回、schema変更、更新履歴の不足では正本から別世代へ再構築する。snapshotの複数commitに、ロード済みノード・関係数とsnapshot位置を記録する。中断したloading世代をactiveへ切り替えない。件数の照合と差分取込みを終えたready世代だけを使う。

ArcadeDBのheap、page cache、WAL flushとコンテナ上限はComposeで固定する。評価条件と実測は[検証記録](../progress/2026-10-15-issue-10-evidence.md)を参照する。WAL flushを無効にして得た速度を、永続化した場合の結果として扱わない。

通常はArcadeDBを永続volumeのまま再起動し、APIの位置と完全性を確認する。正本をbackupから復元した場合は、以前の位置が存在しないことを検出して再構築する。手動で完全に作り直す場合はGo APIを停止し、対象が`KOUJOU_GRAPH_DATABASE`の派生DBであることを確認してから、そのDBだけをArcadeDBの管理操作で再作成し、APIを起動する。PostgreSQLや他の物理DBは削除しない。

正本のbackupを優先する。ArcadeDBのnative backupを使うときは、`BACKUP DATABASE 'archive.zip'`の完了後に保存先とarchiveを確認する。復元は停止した投影writerと分離したDBで行い、ID・固定版・関係・位置を照合する。file URLによるrestoreは既定で拒否される。試験用Composeだけがローカルrestoreを許可し、標準サービスでは許可しない。実運用の復旧は[公式手順](https://docs.arcadedb.com/arcadedb/how-to/operations/restore)に従い、使う26.10.1で対応を確認する。

## 検証

`pnpm run test:graph`は分離した実PG・ArcadeDBで、HTTP探索・入力確定・認可・更新・削除・応答喪失・stream切断・途中停止・並行再構築・WAL再起動・native backup/restoreを確認する。既存の開発用volumeは使わない。

`pnpm run test:graph-load`は合意したWindows環境で評価データを再生成し、4探索と4更新、cache内と実コンテナ再起動後を測る。HTTP/JSONとSoT認可を含み、外部Ory認証の時間は含めない。データサイズ、JVM設定、コンテナのメモリpeakとI/Oを`test-results/graph-load.json`へ出す。再起動はArcadeDBのpage cacheを空にするが、OS全体のfile cacheは消去しない。別OSの機能試験を、このWindows端末の性能達成として数えない。
