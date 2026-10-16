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
