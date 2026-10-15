# Workspaceと端末の確認

認証の起動は[認証手順](authentication.md)に従う。採用した役割・招待・失効は[ADR 0005](../adr/0005-workspace-device-access.md)を参照する。

## 共通画面

ログイン後の「チームと端末」でWorkspaceを作成する。作成者はそのWorkspaceの管理者となるが、実行依頼は初期状態で不許可になる。参加者・端末の管理画面で役割・実行許可・利用停止と、端末の入力取得・報告許可を別々に設定する。

招待は管理者が役割を選んで作成する。ログイン済みのリンク所持者が一回だけ受諾でき、リンクは7日で期限切れになる。転送先も参加できる。招待を開いてからログインする場合、受諾待ちの招待だけをsessionStorageへ保持し、受諾後に消す。OAuth資格情報は保存しない。

Windows版では「この端末を登録」から、アプリの識別子・登録者とWorkspaceごとに資格情報を登録する。初期の入力取得・報告は不許可で、管理者が明示変更する。資格情報はAPI接続先・Workspace・Userに対応するDPAPIファイルへ置き、共通UIへ値を返さない。「資格情報を更新」で旧資格情報を失効し、「接続を確認」で最後の接続報告を記録する。

「端末を失効」、登録者の離脱・そのWorkspaceでの利用停止は、該当Workspaceの新しい操作を拒否する。利用停止はそのWorkspaceの所属に対する操作で、別Workspaceの所属を停止しない。開始済みCLIの停止は保証せず、画面には最後に観測した時刻を示す。再登録後も、失効前の入力取得・報告許可を自動で復元しない。

## 検証

`pnpm run test:access`は分離したPGで認可表・招待・失効・再送・streamロック後の世代再確認・秘密値非公開を試す。`pnpm run test:auth`は実OryからUserを構成し、BFF CSRFとWorkspace作成の境界を確認する。

Windowsの対話試験は検証用アプリを閉じ、起動したAPI/Oryに対して実行する。`KOUJOU_NATIVE_TEST_FILE`で、本人が検証用として選んだOAuth用DPAPIファイルを明示する。コピーを並行使用せず、同じファイルでrefreshし、元のログインは保持する。

```powershell
$env:KOUJOU_DEV_CA_FILE = Join-Path $PWD '.cache/tls/cert.pem'
$env:KOUJOU_NATIVE_TEST_FILE = '<検証用OAuthファイルの絶対パス>'
cargo test -p koujou-san-desktop live_workspace_device_lifecycle -- --ignored --nocapture
```

この試験は自身で作成したWorkspaceから離脱して専用deviceファイルを削除し、元のOAuthファイルを削除・ログアウトしない。新しい実行ファイルをbuildする前も旧検証アプリを閉じる。

資格情報は認可されたprivate発行応答でだけ返す。発行・更新のoperation IDと前提版をネイティブ側で保存してから送信し、応答が失われた場合は同じ要求を再送する。共有receiptへ秘密値は保存しない。`KOUJOU_AUTH_KEY`を失う・変更する場合はBFFだけでなく、招待の証明と端末資格情報の導出にも影響するため、再ログイン・招待再作成・端末資格情報の再登録を見込む。
