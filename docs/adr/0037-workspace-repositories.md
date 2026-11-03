# 0037: Workspaceの共有接続とrepoの初期設定

状態: 採用

## 判断

空のWorkspaceからProject、repo、相談を準備できるようにする。Giteaの新規作成資格情報は、Workspace管理者が共有接続として設定し、そのWorkspaceで作成を許可する方式を本人が選択した。

共有接続・repo登録をGo APIで確定し、PostgreSQLを正本とする。接続URLとrepoの外部識別を変更するときは別の登録を作る。Projectへの関連付けは任意であり、WorkspaceやProjectを一つのrepoに限定しない。関連付けによる権限付与、Context注入、clone、CLI起動は行わない。

Giteaのトークンと追加CAは、既存の認証キーから用途別に導出したAES-GCMキーで暗号化し、Workspace・接続IDに結び付けて保存する。資格情報をAPI応答・共有履歴へ返さない。接続の保存、作成許可、新規repo作成、観測対象の保存、外部起票は別の操作とする。repo作成では所有者・名前・公開範囲を明示し、送信意図と一度だけの送信を記録する。応答不明時は取得で照合し、存在確認とその要求による作成の証明を区別する。

既存のGitea/GitHub設定ファイルは維持する。Giteaの登録済みrepoは既存の観測・起票へ接続する。GitHub Appの秘密鍵・インストール許可は運用者が保持し、Workspace管理者は画面から引継ぎ用の設定JSONを出力できる。他Workspaceの許可を自動的に引き継がない。

初期設定のrepo作成を明示APIへ追加する。ADR 0034でRunのCLI/APIへ分離したPR・CI再実行・merge等は引き続きRunへ依頼し、読み取り用チャットtoolへ外部書込み能力を追加しない。

## 影響と確認

migration 026は現在のScopeの種類・名前の索引、共有接続とrepo登録、管理画面で登録した観測対象の区別を追加する。既存のContext・Scope・Run・起票記録は書き換えない。Contextの版履歴は維持し、Scope検索用の現在値は正本から移行する。

トークンの暗号化・出力除外、Workspaceと管理者境界、所有者・公開範囲、一度だけの送信、応答喪失と照合、観測・起票への接続、個別資源権限を専用DBと管理下のHTTPで検証する。実アカウントのrepo作成や実LLM消費を通常試験へ含めない。

手順は[共有接続とrepo](../runbooks/gitea.md)、受入条件と証跡は[導線修正の記録](../progress/2026-11-03-navigation-fixes.md)を参照する。
