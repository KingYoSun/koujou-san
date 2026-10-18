# Issue #29の検証記録

状態: Complete。Scope revision 1。基準main `fb336a0df3ac10f4d6b03c7e9ecfc3db6994c399`。PR #64の最終head `d36ca862`でCI 39113715998の全jobが成功し、独立検品とmerge後tree一致・差分0を確認した。途中の未完了記述は各検証時点の記録として残す。最終判定は[現在状態](current-state.md)を参照する。

## 判断と実装

本人はApache-2.0、新規外部投稿へのDCO 1.1、Linux x86_64の単一Composeホスト、初回接続先設定を持つ未署名Windowsプレビューと手動更新を承認した。LICENSE・NOTICE・DCO・外部投稿の手順とpackage metadataへ反映した。既存履歴にsign-offを付けていない。

Goの管理CLIで新しいprivate保存先と秘密値を生成し、公開TLS入口と内部管理経路を分ける構成を実装している。DB・Ory管理API・ArcadeDB・Bifrostには公開portを設定しない。APIと入口のnetwork namespaceを共有し、APIと管理proxyはloopbackでlistenする。Oryは開発モードを使わない。

backupは書込み側を停止して三つのDBをストリーム保存し、設定・鍵・Gateway領域をprivateなarchiveへ含める。restoreは新規保存先だけを対象にする。更新では先にmigrationだけを実行し、失敗時に旧版とDBを復旧する。新版アプリの起動後には外部作用を理由なく巻き戻さない。いずれも配布物での導入・停止/復旧・更新の受入はこれから行う。

Windowsの初期設定はHTTPS originと認証設定を確認した後にURLだけを保存し、認証情報とは分ける。従来の環境変数接続を保持した。独立Runnerを同梱し、更新・削除時は動作中のRunnerを強制終了しないガードを準備した。未署名インストーラーのbuild・新規導入・手動更新は未確認。

## 途中で確認した結果

- 最初のRust compileで`Result<_, &str>`と`Result<_, String>`の不一致を検出し、戻り値を修正した。`issue29-native-config-check.log`はその後PASS。Cargo.lockは自身の版だけが`0.1.0-preview.1`へ変わり、依存版は変更していない。
- 接続先検証の試験で、usernameが空でpasswordを含むURLが受理されることを確認した（`issue29-origin-before.log`で1件FAIL）。資格情報を含むoriginを拒否し、同じ試験がPASSした（`issue29-origin-after.log`）。この試験の初回にPythonのencoding指定を欠き、対象試験未追加で0件実行した結果は証拠に含めない。
- `issue29-config-unit3.log`はPASS。初期秘密値の生成、既存保存先の上書き拒否、固定版のchecksum、Ory開発モード不使用、管理サービスの非公開、入力データとしてのSMTP URI、archiveの外側へ書かないことを確認した。
- 公開入口の通常path正規化はGoの307だったため、試験の想定301を修正した。秘密ファイルの内容が応答に含まれず、正規化後・encoded pathも公開rootに閉じる条件を確認した。
- Goの初期compileでは宣言前の変数、任意Gateway flagの未使用を検出して修正した。`issue29-ops-unit5.log`は管理packageとcmdの局所試験がPASS。
- 古いnative配布候補のPE importを読み、Windows標準DLLとUCRTへの依存を確認した。新しいインストーラーの依存検証を代替するものとして扱わない。

各結果は記録取得時点の範囲である。対応する五条件と現在の残作業は[計画](2026-10-18-issue-29-plan.md)を参照する。実LLM・GitHub・外部メールへの追加試験は許可枠を取得しておらず、実行しない。

`issue29-native-unit.log`は32件PASS・12件ignore（既存の実接続用）。最初の全体試験では既存sync-storeの1件が5秒でtimeoutし、Goと残りUI63件は成功した。内容を変えない`issue29-ui-retest.log`はUI18ファイル64件すべてPASS。原因は未確定で、並行compileとの時刻関係だけから環境起因と断定しない。

WindowsのWebView2 static loaderは公式NuGet 1.0.3800.47とhash一致を確認した。ラッパーのMITとloaderのBSD-3-Clause、原SDKのnoticeを分け、原文をlegalへ保持した。NSIS 3.11のzipはTauri CLIの固定SHA-1と照合し、原licenseを保全した。これは配布物全体のAC-5判定の代替ではない。

最初のserver配布物のbuildは`issue29-server-build1.log`でPASSし、固定imageとbundleを生成した。新規導入ではPostgreSQL init scriptのmodeが0600で内部postgresユーザーが読めず、rolesが一つも作られない状態を確認した。privateな診断記録にPermission deniedがあり、秘密値を含まないscriptだけ0644へ修正した。既存fileや復旧後にも適用するようchmodを明示した。

失敗時にmainのos.Exitがdeferを飛ばして操作lockを残す点も修正し、lockの取得・解放を終了コード設定より内側の関数へ移した。statusは読取りとしてlockの外で取得する。最初のsmoke環境は所有projectだけを停止し、失敗の証拠を保持した。新しい空の保存先で再試験する。

最初のdesktop buildは原文licenseを含まないcrateで停止した。metadataとVCS commitを照合して六つの不足を特定し、対応する原文を追加した。selectorsはsource headerが指すMPL-2.0の原文と、実際のcrate sourceを配布する。原文のある場所と対象sourceの場所を分離し、license補完で対象sourceを置き換えない。

独立検品で認証用内部proxyの接続先と四つのAPI prefix不足を検出した。`issue29-review-before.log`で失敗し、loopbackへの統一と公開経路の修正後、同じ試験が成功した。Windowsインストーラーの生成は`9f41b311`で成功したが、MPLソースの取得先不足が検品で見つかり、source archiveをインストーラーにも同梱するよう修正した。修正後の再生成と導入確認は未完了。

実際の新規起動ではKratosとHydraのTLS設定形式、ログレベルとhealthcheckの違いも確認した。固定版のKratos schemaと既存の動作済みhealthcheckを照合して修正した。`issue29-server-up4.log`は修正CLIと`ed2c0bea`のimageを組み合わせた途中fixtureで起動成功した結果であり、最終配布物の新規導入の代替にはしない。`issue29-distribution-smoke7.log`は外部配送しないMailpitを使い、管理者初期化・自己登録・メール確認・二人のBFF認証・招待・版履歴・所属・参照関係とBleve/ArcadeDBの結果・反映位置を確認した。

公開proxyでConnection headerが信頼する転送headerを取り除く経路は、`issue29-proxy-before.log`でFAILした。Go ReverseProxyのRewriteへ切り替え、転送headerの除去後に設定する形で同じ試験が成功した。外部からの転送headerを認証接続へ引き継がない。

## 固定配布物での受入結果

Linuxは`server-0.1.0-preview.1-050ae4df`、Windowsは`desktop-0.1.0-preview.1-6189df56`を対象にした。両候補間の`apps/server`差分は0。最終候補`6189df56f6c54eebe7c5916d6d41889ee7c72626`には、初回展開先を作る手順、実archiveを展開するCI、Windowsの埋込み修正を含む。ローカル成果物は公開リリースではなく、CIと独立検品の判定も別に確認する。

| 条件 | 観測した結果と証拠 |
| --- | --- |
| AC-1 | 空のLinux保存先で配布CLI・固定imageを起動。管理者初期化、別利用者の自己登録・メール確認、二人のBFFログイン、Workspace作成と招待受諾を実通信で確認。`issue29-distribution-cycle3.log`。Windows初回画面で所有HTTPS接続先を保存し、再起動後もURL再入力なしでログイン・Workspace入口を表示。`issue29-native-setup-ui.json`。既存Web/Tauriのブラウザ復帰・ログアウトは[#5の証拠](2026-10-15-issue-5-evidence.md)を再利用した |
| AC-2 | API/管理proxyのloopbackと公開入口だけのportを照合。公開から管理API・鍵へ到達しない局所HTTP試験がPASS。固定image、DB/role、issuer、SMTP、鍵の責務は[運用手順](../runbooks/distribution.md)と[ADR](../adr/0029-standard-distribution.md)に記録 |
| AC-3 | Context二版、Scopeと所属、参照関係、未開始Run、二人の認証セッションを含むprivate backupを取得。新規保存先へrestore後、起動サービスはPostgreSQLのみで、検索領域は空。明示起動後、固定記録・履歴・二セッションと、再構築したBleve/ArcadeDBの結果・正本の反映位置が一致。任意Bifrostもprovider未設定・モデル要求0で起動し、backup前後を照合。`issue29-distribution-cycle3.log` |
| AC-4 | #27 API `17def2aa914e0c31cf8a03360521e57864d060d0`とschema 22の保存処理、現行入口とstdin対応の管理コマンドを組み合わせた所有fixtureで確認。過去に配布済みの製品とは扱わない。新規`triage_targets`作成だけを失敗させ、更新コマンドが失敗を返し、旧版・三DB・二セッション・記録と投影が復旧。失敗条件を外した更新では実migration 23が適用され、同じ記録を照合。`issue29-distribution-update3.log` |
| AC-5 | 本人の四判断と原文・固定依存・配布物を照合。最新NSISの新規導入でRunner・LICENSE・NOTICE・依存表示・source archiveの同梱とhashを確認。動作中の所有Runnerを終了させずinstallerがexit 2、終了後の手動再導入と所有installの削除が成功。`issue29-desktop-install4.log` |
| INVAR-1〜3 | DB正本と再構築、既存CLI/repoの移行不要、認証SaaS契約不要、秘密値・backupのprivate保存と非公開経路を上記試験と独立検品で照合。外部LLM・GitHub・実メールへの要求は0 |

Windowsの画面確認で、初期配布候補がファイル一覧を表示する失敗を観測した。Tauriの`custom-protocol`と、絶対Windows pathをURLとして解釈させない相対`frontendDist`を適用し、`http://tauri.localhost/`から埋込みUIが表示されることを確認した。実画面の対象binary SHA-256は`CD5C40969E764C9404AE459DF6B768848E37E2D44C0A7F8B410A75203565AD21`。新規のURL-only profileは検証後に削除し、既存のDPAPI認証情報は読取り・変更していない。

旧Windows候補ではRunnerが二重に同梱され、installed hashが先にbuildしたコピーと異なる失敗も確認した。Tauri標準の追加binary同梱へ一本化し、最終build後のRunnerを照合対象にして解消した。更新fixtureの途中失敗は、現行bootstrapが旧DBを先にschema 23へ更新した点と、失敗triggerが旧版起動の`CREATE TABLE IF NOT EXISTS`まで拒否した点だった。schema 22の保存処理へ揃え、拒否対象を新規tableへ限定してから上記の成功を確認した。WSLの空portがWindows側の予約portに重なった失敗は、所有試験のportを明示して解消した。製品の公開URL設定や失敗保護を緩めていない。

新しいWindows配布境界だけを実画面で確認した。実S3スリープと人による初見操作は、本人承認どおり未確認のまま#33に保持する。本Issueの結果で代替しない。
