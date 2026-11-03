# Gitea OAuth接続の所有者取得を修正する

## 範囲と条件

基準commitは`7eaf322b`。通常UIでOAuthアプリ登録、本人認証、接続共有まで完了した後、「所有者を取得」が失敗した。Gitea 28.2.0の`GET /api/v1/user/orgs`はscope不足で403を返している。本人所有のrepoを作る場合も、この一覧取得を通る。

ユーザーは`read:organization`の追加、修正・検証・再配備と、既存接続の再認証後のrepo作成継続を承認した。組織・チーム情報を参照できる権限であり、[公式仕様](https://docs.gitea.com/development/oauth2-provider/)に従う。変更は認証に関わる区分Cとして、固定commitの独立検品を行う。

| 条件 | 対象と期待結果 | 判定方法 |
| --- | --- | --- |
| AC-1 | 新しいOAuth認証が組織一覧の参照権限を得て、本人の所有者候補を取得できる | 所有HTTPが実際の要求scopeを記録し、権限のない組織一覧要求を403にする専用DB試験。修正前の失敗と修正後の成功を確認 |
| AC-2 | 保存済み接続を通常UIで再認証し、所有者選択から依頼済み非公開repoの作成・Workspace接続まで完了する | 既存の同一接続IDの再認証試験と、実環境の通常UI操作。個人環境の証拠はGit管理外へ保存 |
| INVAR-1 | 既存の権限確認、PKCE、秘密値の暗号化・出力除外、作成許可、既存PAT接続を維持する | 既存の`test:forge`と差分の独立検品 |
| INVAR-2 | 保存データとschemaを変更せず、既存のOAuthアプリ・接続を再作成しない | 実装差分、配備時の確認と通常UIの再取得 |

OAuth要求のscopeを修正し、所有者取得や作成の経路は維持する。専用DB試験の所有HTTPをscope判定に対応させ、既存試験へ所有者取得と作成結果の確認を加える。手順書に必要scopeと既存接続の再認証を追記する。UIの追加、作成経路の迂回、承認したscope以外の追加、実LLMの呼出しは対象外。

## 本人repoの作成権限への対応

PR #96の必須CIと配備は成功し、通常UIで既存接続の再認証と所有者取得を確認した。続く本人repoの作成は`POST /user/repos`のscope検査で403となり、AC-2は未達だった。

Gitea [v28.2.0のrouter](https://github.com/go-gitea/gitea/blob/v28.2.0/routers/api/v1/api.go)は、このPOSTにuserとrepository両カテゴリのwrite権限を要求する。2026-11-04、ユーザーは`read:user`を`write:user`へ広げ、製品を修正・検証・再配備して作成を続けることを承認した。`write:user`はユーザー設定等の変更にも及ぶ。必要scopeを`write:user write:repository write:issue read:organization`とし、これ以外の権限は加えない。

条件は上記AC-1/AC-2とINVAR-1/INVAR-2を維持する。判定方法には、所有HTTPで実際の要求scopeを認可コードから引き継ぎ、`write:user`なしの作成を403にする試験を加える。既存接続の再認証後、APIの受付成功だけでなく、保存された作成済み状態・非公開設定・repo ID・一回の送信を確認する。修正前の失敗と修正後の成功、区分Cの固定commit検品、CI、配備後の通常UI確認を行う。

## 現在の判定

In progress。[独立検品](2026-11-03-gitea-owner-scope-inspection.md)で`43bd558`の実装差分にblockerがないことを確認した。修正前は専用DB試験の所有者取得が503となり、修正後は`test:forge`の所有者取得・実フォーム・再認証・既存PAT試験が成功した。静的検査も成功した。Windows環境では既存のGit管理外Goファイルを検査中だけ退避し、pnpmの起動先をCJSへ指定して`check`の全処理を実行した。

本人repoの作成に必要な`write:user`への修正を進めている。実装試験と検品の成功を、実環境での作成完了の証拠に代えない。以後のCI・配備・通常UIの現在の判定は、この記録を参照する後続PRの結果欄へ集約する。
