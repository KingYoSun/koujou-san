# Issue #9の独立検品

## 対象と入力

- 固定実装commit: `e0ab4c51cca41ca6dbc72f35997c1e03f2435837`。
- 基準: `a744639ee4aa7084e20a031fb114e4dd072c385b`。Issue #9、Scope revision 1、区分C。
- 担当: `issue9_independent_review`、2026-10-15。実装者の会話履歴を継承しない別コンテキストで実施。
- 入力: Issue、計画、ADR、検索手順、固定コード、実PG/Bleve/HTTPの観測、正本の負荷JSONと未達2回のJSON。DB・既存ブラウザ・私有資格情報へのアクセスは渡していない。
- CodeGraphを使い、読取専用でコード・fixture・提供観測・保存JSONを照合した。

## 条件ごとの結果

| 条件 | 確認した経路・証拠 | 判定 |
| --- | --- | --- |
| AC-1 | 固定40検索、四粒度、種別・ID・版・タグ・直接Scope、許可済み39候補の継続 | PASS |
| AC-2 | Workspaceと固定子参照の現在認可、返却直前の再照合、許可候補のcount、暗号化し主体・条件・世代・位置へ結び付けたcursor | PASS |
| AC-3 | 作成・新版・所属解除・削除・古いevent拒否。2件目のbatch停止後のcheckpoint永続化と実close/open、pendingから再開後の両版・件数一致 | PASS |
| AC-4 | 別世代snapshot・差分・件数と版の検証後切替。marker書込失敗で旧世代を維持し、再構築中の本文・所属変更を反映 | PASS |
| AC-5 | 停止・遅延・部分・必要版欠落からのManifest確定を拒否し、既存Manifestを維持 | PASS |
| AC-6 | 正本JSONと保存JSON、初回・途中の未達記録を照合。合意した全目標を達成 | PASS |
| INVAR-1〜4 | Bleve常設、PostgreSQL正本、直接所属、候補の明示選択・SoT固定版照合を維持 | PASS |

## 保留と再確認

`7653b35`の初回判定は、反映途中停止と再構築中の所属変更が未観測のためINCONCLUSIVEだった。具体的な実装blockerはなかった。既存末端条件の観測を追加した`e0ab4c5`で、差分とAC-3/4を再確認した。Viteの/searchも既存apiProxyを共有する一行の追加で、直接APIとWeb proxyの未認証403という提供観測と整合した。

## 判定

総合判定はPASS、blockerなし。追加差分の`git diff --check`も成功した。検索のproductionコードと負荷fixtureは前回の固定commitから変わらず、全目標の達成判定を維持した。

検品者は試験を再実行しておらず、提供された実測を検品者の独立実行成功とは扱っていない。証跡文書・ADR確定、実際の必須CI、merge後照合はこの独立検品とは別に完了確認する。以後は文書と測定JSONの保存のみ。
