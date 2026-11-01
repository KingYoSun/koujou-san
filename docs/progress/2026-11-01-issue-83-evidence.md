# Issue #83の検証記録

条件と現在の判定は[実装計画](2026-11-01-issue-83-plan.md)へ集約する。ここには実行済みの事実を記す。未確認の操作を完成と数えない。

## 実装と契約

Gitea専用transportを既存の観測・起票へ接続し、保存・ページ取得・Worker・Run・Workflow・Hopper・Triageを共用した。GitHubの保存JSONと`github:<target ID>`を維持する。Giteaは`gitea:`付きIDとinstanceを保持する。[ADR 0034](../adr/0034-gitea-integration.md)と[導入手順](../runbooks/gitea.md)を参照する。

変更前はGiteaだけの設定が無視され、追加した環境読込み試験が失敗した。変更後は同じ試験が成功した。明示CA、誤ったCA、別originへの認証非転送、同名対象、認可、Webhookによる再取得、保存済みGitHub参照を契約試験で確認した。実体の通常Issueが返す`pull_request:null`も扱う。

## 専用実環境

本人がこのPCの専用環境を選び、一般ユーザー・非公開repo・限定scope tokenと、repo限定Runnerの登録・CI実行を許可した。本番環境は使用していない。

| 対象 | 実体 |
| --- | --- |
| Gitea | 28.2.0、`https://localhost:58109`と`:58110`、明示CAでTLS検証 |
| repo | 両実体の`issue83-proof/private-proof`、repository IDはいずれも1 |
| 観測用 | `read:repository / read:issue` |
| 起票用 | `read:repository / write:issue` |
| 操作用 | `write:repository / write:issue` |
| Gitea Runner | 5.2.0、Linux専用コンテナとWindows一時プロセス。OSサービス登録なし |
| ネイティブRunner | Workspace `mcp-one`、登録repo `issue83-private`。Gitea Runnerとは別の登録・実行 |

配布SHA-256はLinux Runnerが`2eb05e7c09701ba194c6a43b8669db7eadf0d5c69b6f59f5710426fd68f82f98`、Windowsが`d2a7c6781123ed9e7b1b903e165cd5d97b4df8926c98c90615c33159ec2b8812`で、公式値と一致した。秘密値・登録ファイル・CA秘密鍵はGit管理外に保存した。

両実体で同じIssue番号1、別のcommit/statusを観測し、取得元URL・repo ID・Gitea版・取得時刻を照合した。製品の起票処理で各実体のIssue #2を作成・再取得し、marker検索の反映待ちでも再作成しなかった。限定scopeでラベル追加・削除、timelineのactorと追加/削除を確認した。

## チャットから固定入力・実装

共通Web UIで相談を保存し、選んだAI応答の範囲をContextへ採用した。会話HTTPは管理下の応答であり、実モデルによる会話とは主張しない。実装と独立検品は、本人指定のChatGPTサブスク接続で`gpt-6.1-sol`を要求したCodex 0.162.1の別実行である。応答に実モデル名がないため、要求名を実モデル名として補っていない。

| 固定対象 | 参照 |
| --- | --- |
| 会話 | `chat-thread:1e32ac60-2fe8-40f5-9657-cf62c833d9e5@4TZIJCPRCFIPRV4Q7T2BRIFXEC` |
| 採用Sentence | `chat-b4ced52313b8c9ddc57e00cf1a4a9c8a7fda54c8c2e593948d8b9e8a9bce813f@IXNPNANVYTWBGH6SHDKOM7D4OY` |
| Run定義 | `run-definition:fc12b7e6-0ed2-44cd-a20f-ae31c7b886d4@R6R7FVGC4EIKBI425CXBS5HV2T` |
| 実装Run | `run:issue83-implementation-verified-exit@IOGIPQ4OTHQ4WOAH3C4IXRCL2U` |
| 固定入力 | `run-input:issue83-implementation-verified-exit@GRWEFZNQAIOORL234JQNK3KJSY` |
| 基準commit | `7e51931c8cebba5d526d401f8eeb869bad9799d6` |
| 修正commit C1 | `a33669c339fb45cb302fbcfcfbe437cc0b3cd994` |
| math.mjsのSHA-256 | `8291914f0c5ebf6be90eaabed2a9d0e638d11de717a548dfa9421543468fd61f` |

入力画面で選択したSentenceの本文だけを確認した。stdinへbindingしたcontextとpromptは共有の固定Run入力と一致し、Sentence一件の本文も一致した。Context検索と会話履歴の自動注入は使わず、LFSの本体は渡していない。

最初の実装試行はモデル応答とローカルcommitを得たが、検証用Nodeラッパーのstdinが開いたままで終了しなかった。終了処理を直し、その試行はexit -1の失敗として残した。同じ固定Contextから新Runを開始し、ローカルbuild/testとexit 0を確認した。失敗Runの結果を書き換えていない。

## 実PR・Linux / Windows CI・成果物

専用PR #3の失敗からC1へ更新し、さらに検証文書だけのC2 `42351c8c7353c277a70ee2e5de47800867308441`へ進めた。

| run / attempt | commit | 結果 |
| --- | --- | --- |
| 1 / 1 | `27dfcd8f30697180465845d244971d06f813ff17` | LinuxはLFS取得後に予定したsumテスト失敗。Windowsは公開Actions取得のCA不足 |
| 1 / 2 | 同上 | Linuxはsum失敗。WindowsはSchannelの明示CA指定不足 |
| 2 / 1 | `7e51931c8cebba5d526d401f8eeb869bad9799d6` | 両OSの非公開checkout・LFS取得成功、sum失敗、artifactはskipped |
| 2 / 2 | 同上 | 既存Runから再実行を依頼。Linuxはsum失敗、WindowsはRunner自身のPATHによるGit Bash選択失敗 |
| 3 / 1 | C1 | 両OSのcheckout・LFS・build/test・artifact成功 |
| 5 / 1 | C2 | 両OSで同じ工程が成功 |

Windows RunnerのプロセスへGit BashのPATHと、公開CAを残した私設CA bundleを指定し、TLS検証を維持した。GiteaのPR runではbranch検索が空となる契約も確認し、未確認のbranch絞込みを製品へ追加していない。

LFS素材は65,536 bytes、OIDは`04f1c31abafc18079243f47ab01a59a40bdcd4a89389e16d4519a25640838fb7`。両OSのartifact ZIPをAPIから取得し、commit・platform・LFS SHA・`passed:true`を照合した。署名付き同一instance URLへのredirectでは認証headerを転送しなかった。

| run | Windows artifact / ZIP SHA-256 | Linux artifact / ZIP SHA-256 |
| --- | --- | --- |
| 3 | 1 / `562eb56be8054073ea8874f9be1d95fe37a1f0142898f44fe42bc98a0e62c8d1` | 2 / `f93007095a4e4a28bfc68a436eca248377b58602fdd0685a457c6ece1806674f` |
| 5 | 5 / `40b03a417ccc8c45bfcc80ac2f85cc62ff6c3f6a12ecf9406dddc1f7848b795c` | 6 / `3fcbe7e869335a715fba20ebc2f189d334296073f1ea1cc42e1282b69e7f25bc` |

## 独立検品と既存工程

C1は`run:issue83-inspection-c1-fixed`、C2は`run:issue83-inspection-c2`で、固定snapshot・条件・機械照合参照だけを別のephemeral Codexへ渡した。Contextはnone、実装会話・自己評価・前Runの出力は含めない。sumの仕様、変更path、LFS pointerの維持をそれぞれ検品し、両Runともプラットフォームが`assessment.state=valid / verdict=PASS / validity=current`と確認した。

C1の検品statusは`run:issue83-c1-inspection-status`から公開し、Giteaで再取得した。PR #4は`run:issue83-create-pr`から作成・GET確認した補助検証である。C2への更新後、C1を固定した`run:issue83-stale-fixed-head`は現在のPR head不一致でAPI書込み前に失敗した。新headの検品status公開と保護規則によるmerge可否は、承認待ちの検証に残す。

`workflow-execution:issue83-ci`は実Gitea run 5とC2、必要jobのLinux / Windowsを固定してPASSとなった。Hopperは`kind=gitea`の実失敗観測を、過去の発行しない評価へ渡した。Triageには`origin=gitea`、起票接続とIssue番号、既存優先度ラベルを保存した。自動同期の完了とはまだ主張しない。

## 回帰と画面

Go通常試験、GitHub/Gitea観測の専用DB、起票の専用DB、Workflow、Hopper、Triage/automation、Runの専用DB試験が成功した。共有Gitea接続への並行要求でRetry-Afterのdata raceを再現し、atomic更新後にtransport・観測・起票のrace検査が成功した。クライアントは109テスト、型検査、Web/Go buildが成功。`check:desktop`も成功した。区分Cの固定実装commitの独立検品と最終CIは別に行う。

rootの`pnpm run check`は、既存のGit管理外`apps/server/.cache/issue33-config/main.go`のgofmtで停止した。この作業の無関係なファイルは変更していない。clean checkoutのCIと、変更対象の整形・Go検査を確認する。

共通UIで二instanceの同名Issue、PR更新、実行回、成功/失敗/未観測と固定入力を確認した。原記録へ移動し、チャットの関連Runで終了コードと独立検品PASSが別々に表示されることを確認した。画面証跡はGit管理外の`test-results/issue83-*.png`とDOM記録に保存している。
