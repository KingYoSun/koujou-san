# Issue #8の検証記録

- 対象: Issue #8、Scope revision 1、区分C。
- 基準: `70996e6669d8f85ef23b9e0f71337f6d74111e02`。
- 実装の固定commit: `27e22258e65ff1b411161f0f5f248b309d6ec85d`。
- 現在の判定は[現在状態](current-state.md)へ集約する。

## 条件と観測

| 条件 | API・保存・実画面の観測 |
| --- | --- |
| AC-1 | 実PGで107件を100/7件の固定snapshotへ分け、途中更新、重複・遅延した差分、取得者の変更を照合。2アカウントのChrome/Tauriで同じ本文・Scope・所属版を表示し、APIプロセスの停止・再起動後も一致した |
| AC-2 | 実PGで同じContext・所属版への同時要求は200/409となり、明示再適用後に一致。実画面ではWindowsの未送信下書きを残してWebで本文を更新し、再起動したWindowsが最新版と自分の下書きを競合として表示。再適用後、両画面で本文と版`LJNSQ7YVACRFA2FWZV4K35HLG4`が一致した |
| AC-3 | ローカルTLS中継でWindowsのContext保存成功直後の応答を破棄。Webは新しい本文を取得し、Windowsは結果不明の内容と操作IDを保持した。通信復帰だけでは再送せず、同一要求の手動再送後に版`LHLVG77JKTMREXCZAQFXDDRZWB`へ一致。本文履歴は保存前3件、応答破棄後4件、再送後4件だった |
| AC-4 | Windowsの切断中は最終同期時刻・古い可能性・未送信を表示し、送信を抑止。再起動して下書きが復元された。WebもAPI停止中にScope名を編集でき、復帰後も未送信のまま保持。本人の送信後に両画面のScope版`OIACDYMMMASOSG66V4IRHZHPVH`が一致。Web再読み込みでも下書きを復元し、サインアウト・同一ユーザー再ログイン後は削除されていた |
| AC-5 | 実PGでcursor不明・履歴欠落からsnapshotへ復旧。実画面ではWindowsを切断し、Webで所属を2回更新した後、検証Workspaceの中間履歴1行だけを削除。再接続したWindowsは完全差分と扱わずsnapshotを経て、Webと同じ所属版`USWJGBCCCIIK77VQTHI2TYFOG3`を表示した |
| AC-6 | 実PGでDevice担当・権限、1/3/2・重複・遅着・旧summary不変、位置が変わらない権限取消、Workspace失効を照合。実Device資格情報で1/3を送り、Webは連続1・最終3・欠落ありを表示。2の遅着後は連続3・欠落なしとなり、実行結果は常に未確認。Windowsの未送信所属下書きを残して参加権限を失効させると、本文・下書きと該当DPAPIファイルが削除され、別Workspaceと元OAuthは継続した |
| INVAR-1〜4 | 更新位置・通知を本文の正本にせずAPIへ再照会。受付、観測済み報告、実結果を区別した。報告はContextへ注入せず、Run開始・割当・外部起票・pause回答を下書き送信へ入れていない |

画面確認はWindows 11、Chrome拡張とWindows Computer Use、Tauri release buildで実施した。Tauriのウィンドウは1003×791、ライトテーマ、日本語。Webは同じ共通UIを使い、別の検証ユーザーで操作した。選択メニューはキーボードでも選択・確定できた。開始操作・CLI実行・実Runは未実装であり、報告fixtureから成功を推定していない。

## 実行した検証

- `pnpm run check / test / build`: 成功。通常Goテストと、共通UI・契約5ファイル24件が成功。
- `pnpm run test:sync`: 分離した実PGで成功。旧001〜004データから005への移行でも本文と対応sequenceを保持した。
- `pnpm run test:database / test:auth / test:access / test:contexts`: 成功。実Oryの認証・復旧・任意TOTP、既存PGの停止・復元、認可とContext解決を維持した。
- `pnpm run check:desktop / test:desktop / build:desktop`: 成功。通常nativeテストは3成功・4 ignored。ignoredの同期fixture作成は実際に別実行して成功し、報告送信も1/3/2それぞれ別実行で成功した。以前の認証・端末ライフサイクル用ignoredテストは本Issueでは再実行していない。
- Windows DPAPIの保存・置換・再起動・削除、API/User/Workspaceの分離をnativeテストと上記の実画面で照合した。

## 修正前の失敗と修正後の確認

1. 初回の実画面ではsnapshot後の取得済みIDにrevisionも送信し、厳密なAPIが拒否した。送信キーがkind/idのみというテストを先に失敗させ、Identityへ変換して修正。再buildしたTauriとWebの初回同期が成功した。
2. 再接続後も通信断の案内が残る条件をテストで再現し、復帰時の下書き・競合・結果不明に応じた案内へ修正。再読み込みしたWebとrelease版Tauriで、通信復帰後の案内を確認した。
3. 独立検品で報告の`artifact_revision`から端末パスを共有できる漏れを指摘された。版だけを`C:\private\log`へ変えるHTTPテストは修正前に200/want400で失敗した。artifact IDと同じ論理識別子の検証を版へ適用し、同じ試験を含む`test:sync`が成功した。適用済みmigrationは変更していない。

型検査中のUUID関数型と未使用importのエラーも修正し、最終checkは成功した。native linkerのメッセージはwarningで、コンパイル・試験・buildの失敗はない。試験中の旧中継プロセスでclose listener警告が出たため、中継スクリプトの重複登録を除去した。

## 証跡と後処理

ローカル証跡はGit管理外の`.cache/issue-8/`に置いた。`native-restored-conflict.jpg`、`native-response-unknown.jpg`、`native-response-replayed.jpg`、`native-history-recovered.jpg`、`native-workspace-revoked.jpg`、`native-scope-synced.jpg`と、`web-*.txt/png`が対応する表示を記録する。原資格情報・秘密値は共有記録へ保存していない。

検証専用Workspaceと報告対象を使い、既存Workspaceや元管理者の資格情報を変更していない。検証用Deviceは権限失効で無効になった。検証用ブラウザはサインアウトし、新規作成した検証用Ory identityと私有資格情報ファイルを削除した。監査履歴は残した。TLS中継を停止し、APIを標準の8080へ戻した。

仕様は[ADR 0007](../adr/0007-shared-sync.md)、操作と接続境界は[同期手順](../runbooks/shared-sync.md)を参照する。固定commitの[独立検品](2026-10-15-issue-8-inspection.md)とCI・マージ後照合は別に記録する。
