# Issue #5の検証記録

- 対象: Issue #5、Scope revision 1、区分C。
- 基準: `89982324415fecbe5339874e198f5058ca5d07ac`。
- 現在の判定: [現在状態](current-state.md)。

## 実装と確認結果

採用方式と期限・失効範囲は[ADR 0004](../adr/0004-ory-authentication.md)、再現手順は[認証の開発手順](../runbooks/authentication.md)を参照する。CodeGraphでは新規authファイルに索引一致がなかったため、実ファイルを直接確認した。新しい索引は作成していない。

| 条件 | 観測結果 |
| --- | --- |
| AC-1 | 分離した実Oryで明示管理者・自己登録・未確認Identity・確認メール・コード復旧・パスワード設定・新パスワードlogin・任意TOTP登録と第二要素を確認。Chromeで検証管理者のメール確認、ログイン、設定画面とプロフィール保存、WebログアウトのOAuth revoked/SSO ended、メール復旧から再設定画面への復帰を確認。Windowsのログイン・設定画面起動・ログアウトはユーザーが実画面で成功を確認した。Webの新しいパスワード入力・保存は本人への引き継ぎ待ち |
| AC-2 | BFFがcodeを交換し、署名/client/nonceを検証。暗号化DB、誤ったCSRF・callback stateの拒否を実API試験で確認。UIへ返すUserとフォームの経路を確認。WebのCookie/通信の最終照合は残作業 |
| AC-3 | Windowsで本番用Authを使い、実Ory認証、DPAPI保存、Auth再生成後のrefresh、同じUser取得、revokeとファイル削除を確認。state不一致・別pathは400、期限到達はlogin_expiredで保存なし。Tauri実画面のログイン・設定画面起動・ログアウトはユーザーが成功を確認した |
| AC-4 | Oryの正常access token・実期限到達、ID/refresh token拒否を確認。issuer/audience/scope・用途・inactiveの各負例はintrospection応答fixtureで拒否を確認 |
| AC-5 | 実Hydra/Kratosを試験projectだけ停止し503、再開後200。refresh失敗、BFF期限切れ、Kratosだけの失効後も既存OAuth有効、現在のBFF・OAuth・Kratosの個別結果を確認 |
| AC-6 | 同一メールの異なるissuer/subは別User。表示名の更新でも内部IDが不変で、一般登録者へ管理者を付けないことをDB統合試験で確認 |

`pnpm run test:auth`は実Oryの正常・復旧・MFA・停止試験まで成功した。登録時と手動確認時のメールが並行配送される場合は、試験が未使用メールのコードを順に照合する。復旧メールは件名で確認メールと分ける。パスワードやコードは試験結果へ出さない。

Windowsの`live_ory_browser_restart_refresh_logout`は成功した。保存対象は専用の`.cache/auth-fixture/native-live.dpapi`で、試験終了時に自身のログアウトで削除した。標準開発環境では`admin@koujou.test`を検証用管理者として明示作成し、ブラウザでメール確認した。実ユーザーの認証設定は変更していない。

Windows画面操作は最初にcua_replからskyを呼び出して接続できず、利用不可と案内した。その後、computer-useスキルの指定するnode_replから接続し、起動したTauriウィンドウを取得できた。設定全体の不備ではなく呼出し先の誤りだった。ブラウザのパスワード変更は操作規則により本人へ引き継ぐ。

`pnpm run check / test / build / check:desktop / test:desktop / build:desktop`は成功した。通常UI試験は3ファイル12件、通常native試験は2件成功・対話試験1件ignore。上記の対話試験は明示的に実行して成功した。`dev:desktop`と既存`dev:web`の1420番ポート競合は起動を中止し、buildしたWindowsアプリで確認した。

## 残作業

Web通信/Cookie/保存領域の照合、復旧後のパスワード設定を含む最終画面確認、TOTP登録者のアプリ復帰の追加照合、独立検品後の差分確認、PR CI、マージ後照合を終えてから#5をCloseする。ここまでの結果を全AC完了としては扱わない。
