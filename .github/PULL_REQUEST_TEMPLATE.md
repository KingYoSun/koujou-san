## 概要

<!-- 具体的な問題と、変更後にできることを書く。非該当欄は節ごと削除してよい。 -->

## 対象と条件

<!-- Issue または直接のユーザー指示を参照する。Issue の新規作成は必須ではない。
AGENTS.md と docs/runbooks/issue-lifecycle.md を適用し、固定した末端 AC と PR の対応を記す。
区分 C は Refs を使い、必要な監査・merge 後の確認より前に Issue を自動 Close しない。 -->

- 対象 Issue（Refs / Closes）またはユーザー指示:
- リスク区分 / Scope revision / 基準 commit:
- 対応する AC / 維持する INVAR / 条件と証拠の対応:
- 対象と対象外:
- 最終形で残る処理・統合または削除した処理:
- B / C: 入口から結果・副作用までの経路、共通処理の呼出元、対象の状態遷移:

## 変更前後の証拠

<!-- 不具合は同じ条件の failing-before / passing-after、または実機・表示の観測を示す。
文書・開発環境の変更は、対象記述・参照・command の確認結果を示す。
既存の証拠へリンクし、同じ情報を複製しない。 -->

## UI の確認

<!-- UI 変更がなければ節ごと削除。実装済みの表示・操作と、未確認事項を区別する。 -->

- 対象 surface / platform / viewport / theme / locale / state:
- 同じ条件での before / after:
- 操作、keyboard、focus、accessibility の確認:
- 未確認事項と理由:

## 検証とリスク

<!-- ローカルは変更に関係する検証、全体確認は CI を使う。
失敗・中断・未実行を削除しない。CI 成功だけを AC 達成の証拠にしない。 -->

- 実行 command と結果:
- 失敗・中断・未実行、理由と補完方法:
- CI の対象 commit と結果:
- 挙動・互換性への影響と残るリスク:
- 追加発見の分類（Existing-gap / Regression / New-requirement / Optional-hardening、該当時）:

## 独立監査

<!-- C は必須、B は計画時に定めた場合のみ。A または監査不要なら節ごと削除。
固定した条件と対象 commit から、別の担当または別コンテキストが判定する。
監査後に変更した場合は、その差分と影響先の確認を記す。 -->

- 監査者 / 記録 / 対象 commit / Scope revision:
- AC / INVAR と証拠の照合、blocker、PASS / FAIL / INCONCLUSIVE:
- 監査後の差分確認:
