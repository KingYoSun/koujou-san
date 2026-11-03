# Gitea OAuth所有者取得の独立検品

## 対象と入力

- 対象commit: `43bd5580e1f247e2c03de29f1aac489ed823c6c0`。
- 基準commit: `7eaf322bf1917a46c77b18f568bfe61e4c09da7d`。
- 検品者・実施日: 実装会話を継承しない独立検品担当、2026-11-03。
- 固定条件: [実装記録](2026-11-03-gitea-owner-scope.md)のAC-1、AC-2、INVAR-1、INVAR-2。`read:organization`の追加は承認済み。
- 対象: OAuth要求scope、所有HTTPのscope判定、既存接続の再認証、秘密値の境界、PAT接続の維持。
- 対象外: UI追加、所有者取得・作成経路の変更、他のscope追加、実LLM呼出し、新しい利用条件への対応。

[共通指示](../../AGENTS.md)、個人設定、[文書案内](../README.md)、[開発手順](../runbooks/dev.md)、[Issueから実装する手順](../runbooks/issue-lifecycle.md)、[検品手順](../runbooks/inspections.md)、[Gitea手順](../runbooks/gitea.md)を参照した。CodeGraphでコードを読み、対象commitが作業ツリーのHEADと一致し、管理対象の未コミット変更がないことを確認してから検品した。

固定入力として、専用DBの`test:forge`が修正前の所有者取得で503となり、修正後は所有者取得・実フォーム・同一接続IDの再認証・既存PAT試験に成功した観測と、`check`全処理の成功報告を受け取った。本検品ではこれらを再実行せず、試験の内容と実装経路を独立して照合した。実装会話と自己評価の履歴は入力に含めていない。

## 条件ごとの結果

| 条件 | 確認した経路・証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- |
| AC-1 | `StartOAuth`の要求scopeは既存の3権限に`read:organization`だけを追加。所有HTTPは要求scopeを認可コードに記録し、PKCE検証後にその権限を反映する。`/user/orgs`は権限なしで403となり、追加したAPI試験は本人の所有者候補を確認する。提供された修正前503・修正後成功の観測と整合する | PASS | 実Giteaの確認はAC-2に残す |
| AC-2 | 再認証のUIは選択中の接続ID・現在版を既存の開始APIへ渡す。`AcceptOAuth`はそのIDで保存し、既存試験も同じIDから更新後の接続を取得する。所有HTTP上の実フォーム試験は所有者選択・非公開repo作成まで通る | INCONCLUSIVE | 配備後、保存済み接続を通常UIで再認証し、依頼済みrepoの作成・Workspace接続まで確認する。試験上の成功を実環境の成功に代えない |
| INVAR-1 | 本体の変更は要求scopeの1行だけ。現在の権限確認、S256・state一回使用、資格情報の暗号化、HTTP応答・共有履歴への秘密値の出力除外、作成許可、PAT処理に変更はない。提供された`test:forge`成功と既存試験の確認内容を照合した | PASS | 本検品で実接続や外部書込みは行っていない |
| INVAR-2 | schema・保存処理・既存レコードの形式に差分はなく、アプリや接続の再作成処理も追加していない。Git管理外の配備補助手順にはmigration全件一致と既存OAuthレコードの配備前後照合がある | INCONCLUSIVE | 配備は未実行。既存OAuthアプリ・接続の保持と通常UIでの再取得を配備後に確認する |

## 経路の照合

[OAuth開始・完了・受入れ](../../apps/server/internal/repositories/oauth_sessions.go)では、開始時のWorkspace管理権限、再認証対象のinstance・版、暗号化したPKCE verifier、callback時の現在権限・アプリ版、受入れ時のユーザー・期限・接続版を確認した。再認証も新規認証も同じscope指定を通り、既存の接続IDがある場合はそのIDを引き継ぐ。

[所有者取得](../../apps/server/internal/repositories/repositories.go)は既存どおり接続の取得権限を確認し、本人情報とページ指定した組織一覧を取得する。組織一覧のエラーを無視する変更や、作成経路の迂回はない。[保存とPAT更新](../../apps/server/internal/repositories/service.go)も変更されていない。資格情報はWorkspace・接続IDに結び付けて暗号化され、共有結果には接続情報を返す。PATの空欄による保持とOAuthの秘密値更新は従来の処理を通る。

[OAuth専用DB試験](../../apps/server/internal/repositories/oauth_postgres_test.go)は、実際の認可URLのscopeから組織一覧の可否を決める。固定の成功応答だけで今回の修正を判定する構成ではない。既存のPKCE、権限失効、token更新、暗号化、応答・履歴への秘密値混入の検査も残っている。[PAT専用DB試験](../../apps/server/internal/repositories/postgres_test.go)はPAT認証、所有者取得、作成許可、暗号化とWorkspace間の分離を確認する。[実フォーム試験](../../apps/client/src/workspace-setup.test.tsx)は所有HTTPへ接続し、所有者の取得から非公開repo作成まで操作する。

## 配備前の確認

Git管理外の配備・検証スクリプトを読み取りで確認し、Pythonの構文解析に成功した。実行や配備結果の確認は行っていない。

手順は配布物のcommit・checksum・image revisionと既存migration全件の一致を照合し、private backupを作成した後にAPIとedgeのimageだけを差し替える。設定は既存値を基にし、OAuthアプリ・共有接続・認証sessionの各表は配備前後の全行hashで一致を確認する。周辺サービスの不再起動も確認対象になっている。既存接続を削除・再登録する処理は見つからなかった。これは保持を確認する手順の静的な評価であり、実際に保持されたことの証拠ではない。

## 判定

固定commitのコード・試験差分について、固定条件の未達や今回の変更による回帰に当たるblockerは見つからなかった。全条件の総合判定は **INCONCLUSIVE**。AC-2とINVAR-2の実環境確認が残っているため、作業全体をPASSとは判定しない。

本検品で実行した`git diff --check`は成功した。専用DB試験と静的検査の実行結果は提供観測として扱い、CIと配備後の確認結果は今回の判定に含めていない。対象コードを変更した場合は、その差分が影響する条件を再検品する。配備・通常UIの確認結果は[実装記録](2026-11-03-gitea-owner-scope.md)へ集約する。
