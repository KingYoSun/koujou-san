# 設計判断の記録

ADRには、設計書の方針を具体化した判断と、その理由を記す。製品全体の概要は[設計書](../../koujou-san-design-doc.md)、現在の実装結果は[進捗記録](../progress/README.md)に置く。

ファイル名は `NNNN-<slug>.md` とする。状態は `Proposed / Accepted / Superseded` から選び、提案と確定を区別する。

```markdown
# NNNN: 判断の名前

- 状態:
- 日付:
- 関連する設計書の節、Issue・依頼:

## 背景

## 判断

## 理由と検討した選択肢

## 影響と検証

## 未決事項
```

不要な欄は省略できる。後継判断で置き換える場合は、旧文書に後継先と失効範囲を示す。採用した構成だけで、未実装の機能や未測定の性能を実現済みとして記録しない。

- [0017: Runの割当と専用worktree](0017-run-assignment-and-worktrees.md)

- [0018: 画面から独立したCLI Runner](0018-independent-cli-runner.md)

- [0019: 切断後の実行照合と新Runへの引継ぎ](0019-execution-reconciliation.md)

- [0020: 固定版の工程定義とRun間の進行](0020-versioned-workflows.md)

- [0021: 固定入力での独立検品と証拠の記録](0021-fixed-inspection-evidence.md)
