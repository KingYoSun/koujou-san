# Issue #7の独立検品

## 対象と入力

- 固定commit: `b0b743c39705b333b81cc14b4347350ac375f450`。
- 基準: `491ecd9c6a6640f0f0d857a21069c848019eaf23`。Issue #7、Scope revision 1、区分C。
- 検品担当: `issue7_independent_review`、2026-10-15。実装者の会話履歴を継承しない別コンテキストで実施。
- 入力: Issue、計画、ADR 0006、手順、AC・INVARと実PG/Go/TSの観測。Run実行・外部取得・実エンジン・GUIは対象外。
- 読み取り専用でコード・保存・取得・解決・認可の到達経路を確認した。

## 条件ごとの結果

| 条件 | 確認した経路・証拠 | 判定 |
| --- | --- | --- |
| AC-1 | HTTP保存/取得から型・順序・共有子・固定参照を保持し、型違い・切断参照・循環を拒否 | PASS |
| AC-2 | 所属は内容と別の版付き記録。解除で旧版を残し、親の所属から子へ伝播しない | PASS |
| AC-3 | 新revisionのINSERTと固定子参照。明示更新だけで親が新版を採用 | PASS |
| AC-4 | none・未指定は入力省略。Scope継承なし、noneと直接値の併存を拒否 | PASS |
| AC-5 | 構造と配置を維持し、重複整理・全経路のexclude・抜粋・空コンテナ除去・異版衝突を処理 | PASS |
| AC-6 | 基準の旧版・変更対象・影響Runを別revisionへ保存し、確定済みManifestを上書きしない | PASS |
| AC-7 | 未取得・権限・容量・探索未完了を区別。失敗で完全なManifestを保存しない | PASS |
| AC-8 | Directの値とOriginを保持。Additionは独立した不変記録で、読了をunobservedへ固定 | PASS |
| INVAR-1〜5 | 文書・所属・入力を分離し、未分割文書の自動Sentence化・自動注入なし。取得・展開・Manifestでも現在認可を照合 | PASS |

## 判定

総合判定はPASS、blockerなし。固定差分の`git diff --check`、ContextのGoテスト、TS契約2件を検品担当が独立実行し、成功した。作業ツリーの変更は行っていない。

専用DBの環境変数がない独立実行では実PGテストはSKIPとし、実装担当が提供したHTTP実DB試験の成功結果と実装を照合した。既存database/access/auth回帰、build、CI、マージ後照合は検品者の独立成功観測には含めず、実装担当の[検証記録](2026-10-15-issue-7-evidence.md)で確認する。以後の文書差分は証跡・手順・現在状態の更新のみ。
