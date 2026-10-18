# Issue #29の検証記録

状態: In progress。Scope revision 1。基準main `fb336a0df3ac10f4d6b03c7e9ecfc3db6994c399`。配布物の受入、独立検品、CI、マージは未完了。

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
