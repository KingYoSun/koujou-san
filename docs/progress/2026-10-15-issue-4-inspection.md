# Issue #4: 保存と復旧の独立検品

- 対象: `2823af899dc8f4602c865b89c2ae3424ed2bfdd8`、基準 `c478dd901a3414d9ce824ba9586fe98fa997ae84`。
- 関連: [Issue #4](https://github.com/KingYoSun/koujou-san/issues/4)、Scope revision 1、区分C。
- 検品者: 別コンテキストの `issue4_independent_review`。2026-10-15 07:20〜07:21（JST）。
- 入力: 指示書、Issue、計画、ADR 0003、保存のrunbook、証跡、固定差分とコード。実装者の会話履歴、個人認証、.envの生値を渡していない。

## 判定

| 条件 | 経路と確認                                                                                        | 判定 |
| ---- | ------------------------------------------------------------------------------------------------- | ---- |
| AC-1 | 空DB/bootstrap/初期版再実行のschema一致、既存データ保持、migration失敗の全体rollback、SQL変更拒否 | PASS |
| AC-2 | 同じtransactionのDomain・版・journal・receipt。trigger障害で全て旧状態を保持                      | PASS |
| AC-3 | Workspace行ロック、Actor付きreceipt、前提版。並列HTTP再送/CAS・内容変更・応答喪失・API再起動      | PASS |
| AC-4 | サーバー生成版/位置、commit順の連番、削除、固定ページ範囲、欠落位置/連番の検出                    | PASS |
| AC-5 | 認可が保存/再送に先行し、SQLへWorkspace条件を適用。ID差替え・別Workspace・更新取消後の再送拒否    | PASS |
| AC-6 | DB停止中の拒否とreadiness、DB/API再起動、pg_dump/pg_restore後のDomain・版・位置・receipt一致      | PASS |

INVAR-1〜4もPASS。PostgreSQLを正本とし、投影への個別書込はない。版と順序を端末時計に依存させず、Ory内部テーブルの更新もない。具体的なblockerは0件だった。

独立実行したcheck、test（GoとUI 12テスト）、test:database（実PostgreSQL、-count=1）、build、固定差分のdiff checkは全て成功した。検品前後のHEADは固定commitと一致し、作業treeの変更はなかった。

試験project `koujou-san-db-test-bf58bd1c7f6c` のcontainer・volume・networkが終了後に存在しないことを確認した。生成projectとcontainer labelで制御対象が限定されている。標準DBは前後ともID `179cadb9319c`、15432番、healthyだった。

製品CRUD・Ory認証・snapshot/UIは今回の対象外。desktop検証、GitHub CIの実結果確認、マージ後照合はこの検品では未実行。現在の判定は[現在状態](current-state.md)を参照する。
