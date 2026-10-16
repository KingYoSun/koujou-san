# Issue #13の独立検品

## 対象と入力

- 対象commit: `e9849b1eeac8e01ab0cbc82ca0a4c238aa9519de`と、その後の4ファイル差分`18c26e7bb8157fce88cdd605e8de26514137f2ce`。
- 最終証跡commit: `a799ecc8006fbbb3901f98460bf5d9573aa9d1a2`。18c26e7からの差分は文書・証跡だけで、実装は同一。
- 基準main: `bb0c5cbb26250f2bb56b41193de91a20b32d7505`。Issue #13、Scope revision 1、区分C。
- 検品者: 実装者の会話履歴を継承しない別コンテキストのCodex（`issue13_independent_review`）、2026-10-16。
- 入力: Issue本文、AGENTS、開発・Issue実装手順、[計画](2026-10-16-issue-13-plan.md)、[調査](2026-10-16-issue-13-research.md)、[検証記録](2026-10-16-issue-13-evidence.md)、[ADR 0012](../adr/0012-github-observation.md)、観測手順、関連設計書、固定commitの差分とコード。探索はCodeGraphから開始した。
- 引き継がなかったもの: 実装セッションの会話履歴、資格情報、ブラウザの認証状態。検品者は実装・編集・commit・GitHub書込み・画面操作を行っていない。

## 条件ごとの最終判定

| 条件 | 判定 | 確認した根拠と残る証拠 |
| --- | --- | --- |
| AC-1 | PASS | 実Appの四種類の取得・識別子・版・時刻、共有API、Web/Windows画像を照合。merged PRも区別 |
| AC-2 | PASS | 専用PGで重複・遅延・逆順通知を入力し、現在状態の再取得と同内容の履歴重複防止を確認 |
| AC-3 | PASS | 別commit、必要ジョブのskip、同じrunの再実行、固定attemptを区別。UIの固定試験もsuccessをPASSへ置き換えない |
| AC-4 | PASS | 固定応答で503・401・2ページ目の失敗を確認。保存状態fixtureのUnknown前後でsnapshot・成功取得時刻・版を保持し、両画面とJSONを照合 |
| AC-5 | PASS | 5対象全ての取得時刻が両クライアント終了後に進む。共有・終了中・再接続のsnapshotと版を独立にassert。再起動は先行PG試験で確認 |
| AC-6 | PASS | Appのkukuri限定Read-only、token交換POST 1件・業務GET 9件、範囲外repoのGET前拒否、先行の現在認可試験を照合 |
| INVAR-1 | PASS | Webhookのpayloadを現在状態へ採用せず再取得。失敗時に前回結果と現在の取得不能を分離 |
| INVAR-2 | PASS | CI conclusion・観測state・指定commit/必要ジョブを分離。工程の受入確定・Run生成・Workflow前進は追加していない |

## 独立実行した検証

`pnpm run check / test / build / test:github / check:desktop / test:desktop`は全て成功した。共通UIは35件成功。専用PG試験本体は2.39秒、Compose project `koujou-san-db-test-8435709ccbe1`は削除済み。`TestGithubRemote -count=1`と空白検査も成功した。

18c26e7の追加差分では、merged PRの変換が保存・履歴・API・共通UIまで伝わること、未マージPRは元のstateを維持することを確認した。追加assertを含む`TestGithubRemote -count=1`、共通UIの型検査・35件の試験、差分の空白検査は成功した。

## 実接続証跡の最終照合

追加JSON全体を機械照合し、全対象の終了後の取得時刻が製品クライアント終了時刻より後であること、共有/終了中/再接続のsnapshotと版が一致すること、Unknown前後のsnapshot・成功取得時刻・版が変わらないこと、liveと共有APIのsource_versionが一致することを独立にassertして成功した。App限定Read-only、Web/Windowsの正常・Unknown、Webの履歴とGitHub正本の画像も目視で照合した。資格情報の読取りや再UI操作は行っていない。

Unknownの画面は専用DBの保存状態fixtureであり、実Appを失効させた試験とは扱わない。外部障害と再起動の経路は、先行の固定応答・PG試験と合わせて判定した。

総合判定は **PASS、blocker 0件**。全AC・INVARを判定したため独立検品を終了する。検品者が確認したCI 38836013770はその時点で実行中であり、CI成功・マージ後照合・Closeは実装担当の後続工程として分ける。

この記録は検品者の報告から実装担当が作成し、yomiyasuの意味保持・主述対応・時制の観点で推敲した。検品後に更新するのはこの記録・検証記録・現在状態だけで、コードと受入条件は変更しない。
