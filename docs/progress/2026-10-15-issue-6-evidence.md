# Issue #6の検証記録

- 対象: Issue #6、Scope revision 1、区分C。
- 基準: `0eeee997d5365ad9db1b7af35452102b2f3b12fa`。
- 現在の判定は[現在状態](current-state.md)へ集約する。

## 方式と範囲

ユーザーが採用した4点は[計画](2026-10-15-issue-6-plan.md)、認可表と秘密値・失効の扱いは[ADR 0005](../adr/0005-workspace-device-access.md)を参照する。DESIGN.mdとADR 0014は開始時点で存在せず、設計書と共通UIを根拠にした。

CodeGraphを先に使って既存の認可・保存・起動経路を確認した。索引が一致しなかった新規auth/nativeファイルは実ファイルへ切り替え、その後のアクセス実装は索引からも確認できた。索引の作成は行っていない。

## 現時点の結果

| 条件 | 観測 |
| --- | --- |
| AC-1 | 分離PGで2アカウント・2 Workspace、認証だけでは非所属、転送可能な単回招待、再送、無効・取消・期限切れの拒否が成功。実Oryでも非所属とBFF CSRF・Workspace作成を確認。Webで作成・別Userの受諾・離脱・使用済みリンク拒否、Tauriで作成を実操作した |
| AC-2 | read/get/search/update/executeの認可表で他Workspace・private子リソースを拒否。公開parentと同じWorkspaceでも権限を導かず、明示readだけではupdateできない |
| AC-3 | 閲覧者のupdate/execute、編集者のexecuteを拒否。独立した実行許可を明示変更した場合だけ対象へのexecute判定が成功。端末登録時のinput/reportは不許可 |
| AC-4 | Deviceは許可Workspace・担当対象のinput/reportだけを通過し、他端末・他対象・User偽装・権限変更を拒否。private発行はBearer認証に限定し、汎用native APIの応答から資格情報を返さない |
| AC-5 | 権限取消・離脱・資格情報更新で旧要求を拒否。streamロック待ち中に更新された旧世代も403。別Workspaceの端末接続は継続し、共有応答は失効と最終報告を示しCLI停止を主張しない |
| AC-6 | 実Windowsの本番Authを使う試験で登録・DPAPI保存・インスタンス再生成後の接続・更新・旧資格情報403・離脱後の失効を確認。共有応答・journal・receiptに秘密値なし。Tauri実画面からの登録と接続も成功 |

`pnpm run test:access`、`pnpm run test:auth`、`pnpm run test:database`、`pnpm run check / test / build`、`pnpm run check:desktop / test:desktop / build:desktop`は成功した。通常UIは3ファイル12件、通常nativeは2件成功・実接続用2件は明示実行までignoreする。実Ory試験はTOTPのアプリ復帰まで維持し、認証済みUserをアクセスAPIへ渡す経路も通った。保存基盤の並列更新・停止・backup/restoreも成功した。

`live_workspace_device_lifecycle`はWindowsで成功した。明示した開発用OAuthファイルを本番Authで利用し、秘密値を出力せず同じファイルへrefresh結果を保存した。専用Workspaceは試験で離脱し、そのWorkspaceのdeviceファイルだけを削除した。元の管理者のOAuth資格情報は保持した。

最初のWindows buildは起動中の旧検証アプリによる実行ファイルのロックで失敗した。Computer Useで旧アプリを閉じて再buildし、成功した。

## 実画面と修正の再確認

Webでは専用の合成アカウントを使った。ログインだけではWorkspace一覧は空で、作成したWorkspaceのみ管理者となり、実行依頼は不許可だった。閲覧者の招待を別の合成Userが受諾し、管理用の操作が表示されないこと、離脱で一覧から消えること、使用済みリンクの再受諾が拒否されることを確認した。[受諾後の画面](assets/issue-6-web.png)に秘密値は含めていない。

同じWeb画面から招待リンクのハッシュへ移動した場合に招待が保持されない事象を再現した。`hashchange`の購読を追加し、同じ画面で受諾ボタンが表示されることを再確認した。開発サーバーの更新は明示的な再読み込みで取り込んだ。ブラウザのOAuth復帰後に古いframeがtimeoutした場合は、同じChrome拡張のタブへ接続し直した。

Tauriでは検証用管理者の保存済みログインを使い、Workspaceを作成して端末を登録した。登録時の実行依頼・input/reportはすべて不許可だった。接続確認で最終接続時刻が画面とDBに記録された。画面はCLIの稼働・停止を断定しない。[登録直後の画面](assets/issue-6-native.png)を保存した。

対象未指定のget/search/update/executeも共通Checkで拒否する。実PGの負例で確認した。Context本文・検索処理・Run割当/CLI起動の実装を、この認可fixtureの成功だけで完成扱いにしない。

## CIとマージ後の照合

[独立検品](2026-10-15-issue-6-inspection.md)は適合。[PR #38](https://github.com/KingYoSun/koujou-san/pull/38)のCI [38697335457](https://github.com/KingYoSun/koujou-san/actions/runs/38697335457)は共通UI・Goサーバー、Windows native、秘密値検査がすべて成功した。

検証済みhead `e7235fc1d45f0840493ee81908829de867ed9ce0`とマージ後main `491ecd9c6a6640f0f0d857a21069c848019eaf23`のtreeは、どちらも`a66b837cfb4277e37fcf002ededa51dcdbc8f08a`で差分0だった。最新buildのWindowsアプリを開発用CA指定付きで再起動し、保存済みログインと端末資格情報で接続できた。専用のWeb合成アカウント2件はログアウト後に削除し、元の検証用管理者のログインを保持した。
