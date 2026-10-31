# Issue #82の独立検品

対象は[Issue #82](https://github.com/KingYoSun/koujou-san/issues/82)、Scope revision 1。実装セッションを継承しない別担当`issue82_inspection`へ、固定commit、Issue原文、ADR 0033、必要な仕様と観測結果を渡した。実装者の自己評価で合否を決めず、固定条件の未達と今回の回帰を検品した。

初回対象は`7debc414f0ef30b55c0391869ab913fd41e6b8c8`。再検品対象は`1303be575ab334e18249e7922b0dc2af318f999f`。Windows、専用PostgreSQL、共通UIで2026-10-31〜11-01に実施した。証跡の詳細は[検証記録](2026-10-31-issue-82-evidence.md)、現在の統合判定は[実装計画](2026-10-31-issue-82-plan.md)を参照する。

## 初回指摘と修正

P1、AC-6 / INVAR-3の未達を一件確認した。Workspace編集者でProject閲覧専用の利用者は人間メッセージを保存できなかったが、PutPolicy→Start(chat)→Begin→Finishを通すとAI応答を保存できた。

Start / Begin / Finishの三段階で、通常の依頼者として現在のProject更新権限を確認するよう修正した。修正前の同条件テストが失敗し、修正後は成功した。開始前、開始後から送信前、送信後から保存前の権限失効を含む回帰試験を追加し、検品者も独立に再実行した。初回指摘は解消した。

## 再検品の結果

| 条件 | 判定 | 確認した根拠 |
| --- | --- | --- |
| AC-1 | PASS | Web / Tauriの会話・関連参照の保存、再取得、サービス再生成後の履歴維持 |
| AC-2 | PASS | 実`gpt-6.1-sol`のreceipt、応答状態、既存Hostとoperationへのusage帰属 |
| AC-3 | PASS | 採用範囲・由来・Context固定版・Manifest・Run入力・CLI入力の一致 |
| AC-4 | PASS | 宣言JSONL接続のPause四状態、独立検品結果、管理下CI観測と原記録への遷移 |
| AC-5 | PASS | 応答喪失の照合、再読込後の追加モデル要求0、GUI閉鎖後のRunner継続 |
| AC-6 | PASS | 二利用者・現在権限・送信禁止Scope。実HTTP本文は選択履歴一件と今回依頼のみ |
| AC-7 | PASS | 狭幅、Tab / Shift+Tab / Enter、固定Runへの移動、同一スレッドへの復帰・再読込 |
| INVAR-1 | PASS | 既存Run / Pause / RunnerとWebの実行境界を維持 |
| INVAR-2 | PASS | 実装入力は採用Contextのみ。検品入力は固定成果物・選択済み四条件のみ |
| INVAR-3 | PASS | 現在の権限・固定版・既存方針を経由 |
| INVAR-4 | PASS | 既存共通経路を使い、資格情報を共有記録へ複製しない |

検品者は採用したUTF-8範囲、元メッセージのSHA256、Manifest本文のdigest、固定Run入力とCLI入力、snapshotのJSON digest、Git本文のdigestを再計算した。snapshotと固定Git本文も一致した。Pauseのwaiting / received / delivered / resolvedは同じIDの履歴であり、検品のINCONCLUSIVEと四条件PASSは別記録だった。

最終判定はPASS、追加blockerなし。独立に実行した共通UI107テスト、`pnpm run test:clientops`、差分チェックもPASS。受入条件に残る未確認事項はない。CI表示の証拠は管理下HTTPの合成snapshotであり、外部CI実行成功やGitea接続完了を示さない。必要なPR CIとmerge後照合は別工程として残る。

検品後に加える文書・証跡の差分は、記録とリンクの照合だけを行う。実装コードを変更した場合は、該当差分と条件を再検品する。
