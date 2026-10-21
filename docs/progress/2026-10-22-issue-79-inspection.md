# Issue #79の独立検品

## 対象と入力

- 対象コード候補: `d6d74b6dd0393d51d71cda85fd79fd98cc092223`。基準: `224478a92a73058ff944c6a5985b8f004e5e3412`。
- 検品者: 実装会話を継承しない別コンテキストのAI担当。実施日: 2026-10-22（JST）。
- 条件: [Issue #79](https://github.com/KingYoSun/koujou-san/issues/79)のScope revision 1。本人の選択によりOryは公式修正版を待ち、AC-2全体は未完了として残す。
- 入力: 固定commitの差分、[作業範囲と証跡](2026-10-22-issue-79-evidence.md)、配布manifestと実バイナリ、Node／Go／Windows／Bifrost／配布統合の観測JSONとprobe。実装会話・実装者の自己評価は渡していない。
- 環境: Windows 11、WSL Ubuntu 22.04、Docker Linux、Node 22.23.4、Go 1.27.3。検証先へ実際の資格情報を送らない。

## 条件ごとの結果

| 条件 | 確認した経路・証拠 | 判定 |
| --- | --- | --- |
| AC-1 | package・CI両job・toolchains・patchを含む最低版判定が一致。Nodeの実行版、12本のCERT_REVOKED、通常HTTPSを照合 | ローカル適合。最終headのCIをmerge条件として残す |
| AC-2のGo・配布 | CLI・API・edgeはGo 1.27.3、対象revision、modified=false。manifestの9ファイルとimage IDが一致。実Connection.Call・Edgeの12拒否を補助TLSのx509エラーと対応付け | 適合 |
| AC-2のBifrost | v2.3.2の固定digest・稼働image・Go・CAが一致。再起動後12本を拒否し、通常HTTPS・明示CA付きモデルが成功 | 適合 |
| AC-2の配備CA | 原本だけの更新では配備コピーが変わらず、差替後のコピー・API・edgeのhashが一致。4サービスが再起動し、各コンテナの配布checkで12拒否・通常HTTPS・カスタムCAが成功 | 適合 |
| AC-2のOry | v26.2.0を維持。公式修正版待ちという本人判断と文書が一致 | Issueの残条件として未達 |
| INVAR-1 | Go・Windows native-tls・BifrostでTLS検証、通常HTTPS、明示CA、所有モデルを維持。配布でbootstrap・登録・メール検証・二UserのBFFログイン・招待が成功。送信は公開synthetic値のみ | 適合 |

配布checkが返す抽象エラーだけで証明書拒否とは判定せず、同じGo／CA経路の補助probeと照合した。harnessの期待メッセージ訂正も保存されており、製品不具合の修正として扱っていない。

## 判定

今回のPR対象にblockerは0件。追加の配布統合観測と、evidence・current-state・distribution・server-modelsの文書差分も照合して独立検品を終了した。後続は記録の追加と同版の手順整合で、コード変更はない。

最終headの必須CI成功をmergeの条件とし、merge後に内容を照合する。結果は[PR #80](https://github.com/KingYoSun/koujou-san/pull/80)へ集約する。Oryが残るためIssue #79全体は未完了であり、Closeしない。
