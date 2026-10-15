# 0004: OryによるWebとWindowsのユーザー認証

- 状態: Accepted
- 日付: 2026-10-15
- 関連: Issue #5、設計書§5.2・12.2、[計画](../progress/2026-10-15-issue-5-plan.md)

## 判断

標準構成のOry Kratos・Hydra v26.2.0を使う。ユーザー判断により、自己登録とメール確認、メール＋パスワード、任意TOTPを採用した。TOTPを登録した利用者には第二要素を要求する。メール復旧はパスワードの再設定に使い、登録済みTOTPを削除しない。

GoがKratosのbrowser flowとHydraのlogin・consentを仲介する。対象は登録したWeb/native clientだけで、確認済みの有効なIdentityと必要なAALを検証する。初期管理者は明示したメールアドレスから作成する。自己登録・初回アクセス・メール一致だけでは管理者やWorkspace権限を与えない。

WebはAuthorization Code＋PKCE S256、state、ID Tokenの署名・client・nonceを検証するBFFを使う。ランダムなアプリCookieはSecure・HttpOnly・SameSite=Laxとし、DBにはCookieのハッシュとAES-GCM暗号化したOAuth資格情報を保存する。更新操作はOriginとCSRF tokenを照合する。認証フォームはKratosのbrowser flowのCSRFを維持する。

Windowsは外部のシステムブラウザ、Authorization Code＋PKCE S256、要求ごとのstate、期限5分のloopback callbackを使う。取得したaccess tokenをAPIで確認してから保存する。長期保存はWindows CurrentUserのDPAPIで暗号化したrefresh tokenと接続先だけとし、APIごとにアプリのローカル領域へ置く。UIへtokenや共通client secretを渡さない。再起動時は保存したrefresh tokenで更新する。

APIは要求ごとにHydraのintrospectionでactive、issuer、sub、audience、期限、`access_token`、`koujou` scopeを検証する。続いてKratosのIdentityの有効性・メール確認・TOTP登録状態を確認する。キャッシュによる猶予は設けない。外部IdP移行は今回の対象外で、Userの対応付けはissuer/subの組を一意にする。メール・表示名の更新で内部IDを変えない。

## 期限と失効の範囲

| 資格情報 | 期限と更新 | 失効の反映 |
| --- | --- | --- |
| Kratos browser session | 168時間。MFA登録者のlogin・settingsはhighest_available | SSO・設定・新しいOAuth発行へ次回の照合で反映。既存のOAuth・BFF sessionを連動して削除しない |
| Hydra access token | 15分。BFF/nativeは残り15秒以内ならrefresh | APIの次のintrospectionから拒否。失効時に別client・別sessionまで失効済みと表示しない |
| Hydra refresh token | 168時間。更新結果を保存 | 次の更新から拒否。プロバイダー停止・5xxは認証不能として扱う |
| Web app session | CookieとDBの上限7日 | DBの期限・削除を次の要求で照合。期限だけでは別のOAuth資格情報を失効させない |
| OAuth開始要求 | Web 15分、native 5分 | state・保存期限・callback一致を満たさない要求からsessionを作らない |

Webのログアウトは現在のBFF資格情報のrevokeとアプリsession削除、現在のKratos browser sessionの終了を試す。各結果を別々に返し、確認できなかった失効は`unconfirmed`とする。nativeは自身のOAuth資格情報と保存ファイルを終了し、外部ブラウザのSSOは`browser_session_unchanged`と返す。

## 配置と検証

OryのDB・接続roleを製品DBから分け、admin APIをクライアントから使わせない。ローカルComposeの管理ポートはloopbackだけに公開する。開発用TLS証明書は自動でOSへ信頼登録せず、nativeとViteの開発プロセスにはその証明書だけを指定する。外部公開では有効なHTTPS証明書と公開URLを設定する。

実Ory・分離DBでの登録、確認、復旧、TOTP、refresh、期限、個別失効、停止・再開と、Windows・ブラウザの結果は[検証記録](../progress/2026-10-15-issue-5-evidence.md)を参照する。実行手順は[認証の開発手順](../runbooks/authentication.md)へ集約する。
