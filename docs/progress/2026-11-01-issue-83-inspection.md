# Issue #83の独立検品

## 対象と入力

- 原条件: [Issue #83](https://github.com/KingYoSun/koujou-san/issues/83)、Scope revision 1、AC-1〜8・INVAR-1〜4。区分C。
- 初回対象: `af26f795f3c71e36f51b2913163ffbeaf50a3bd6`。基準: `363c20773cba691a71a8d14b0d315aaf9cd34d5d`。
- 追加差分: `857795ce3caab8de997f06d3b1dc43cac5e9407d`。
- 検品者: 実装会話を継承しない別担当。2026-11-01。
- 渡したもの: 原Issue、関連仕様、固定ソース、[導入手順](../runbooks/gitea.md)、[機械観測](assets/issue83-cycle.json)、選択した固定Run・CI・artifact・配送・再接続・外部mergeの原記録。
- 渡さないもの: 実装会話、資格情報、OAuth/token/鍵、未選択の会話・Context。自己評価を判定の代替にしない。

## 初回の指摘

Giteaの外部ラベル取込みで、採用理由に「GitHubの優先度ラベルを取り込みました」と保存されるP2の指摘が一件あった。実体でも再現し、backendが返す取得元を使ってGiteaと表示するよう修正した。Hopper由来でも接続先を使い、既存GitHubの理由は維持する。

二Giteaの同名対象は確認できたが、GitHub/Gitea間の厳密な同名組合せは検証不足だった。両providerを`fixture/repo`・repository ID 8・Issue 12へ揃えた専用DB試験へ修正した。

初回に未確認だった実Webhook配送、Triage同期、保護規則による旧PASS拒否と新headのmerge、チャット報告は、本人の許可後に実施した。受入条件は追加・緩和していない。

## 再検品

`857795ce3caab8de997f06d3b1dc43cac5e9407d`の独立再検品はPASS、残blockerなし。追加差分へ`test:forge`・`test:automation`を独立再実行し、両方成功した。初回の共通UI 109テスト、desktop 33成功・12 ignored、起票の専用DBとartifact現物照合も有効である。

| 条件 | 最終判定と確認した範囲 |
| --- | --- |
| AC-1 | PASS。明示CA・限定資格情報・許可repo・設定と実行の分離 |
| AC-2 | PASS。同名・同repository ID・同Issue番号のGitHub／二Gitea |
| AC-3 | PASS。実配送202、七対象の切断unknown、再接続後の収束 |
| AC-4 | PASS。起票・PR照合・再実行・status・Runからのmerge結果 |
| AC-5 | PASS。Workflow・Hopper・実Triage同期、旧PASSの405と新headの200 |
| AC-6 | PASS。既存GitHubの理由・保存参照・関連挙動を維持 |
| AC-7 | PASS。共通UI・実両OS Runner・LFS・artifact |
| AC-8 | PASS。採用Contextから固定入力・実装・PR・CI・検品・merge・チャット報告まで対応 |
| INVAR-1 | PASS。終了コード・外部の拒否/成功・CI・検品を分離 |
| INVAR-2 | PASS。現在認可・固定入力/commit・接続先と資格情報を維持 |
| INVAR-3 | PASS。C1/C2検品はContextなし。会話・自己評価を自動継承しない |
| INVAR-4 | PASS。二種類のRunnerを区別し、LFS・artifactは固定参照 |

15件のRun exportについて、SHA-256、固定indexと一巡記録、開始・完了のRun版、レコード数、`complete`を全件照合した。失敗試行も保持している。GiteaのPRとmainはともに`9ccfad9fd083fbc28fb0b38c8c25acb0e7e88bb9`で一致し、WebhookとRunner登録解除の記録も一致した。

再検品担当は編集、実Giteaへの再接続、外部書込み、Run開始を行っていない。本記録の追加差分は文書だけで、検品済みソースは変更していない。

最終head・CI・GitHubへのmerge後照合は、[統合記録](https://github.com/KingYoSun/koujou-san/issues/83#issuecomment-6551788731)へ集約する。
