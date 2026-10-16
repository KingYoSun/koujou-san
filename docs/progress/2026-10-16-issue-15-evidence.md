# Issue #15: 永続要求・WorkerとTriage共有状態の検証

- 対象: [計画](2026-10-16-issue-15-plan.md)のAC-1〜9・INVAR-1〜3、Scope revision 1。
- 基準main: `cdf6c14ad6a97c50db1e385a3714c84fc3102f29`。
- ユーザー判断: 編集可能なP0〜P3テンプレート、通常の手動変更の固定、有効化・自動化主体・枠・予算のWorkspace管理者による管理。
- 確定した構成は[ADR 0015](../adr/0015-automation-foundation.md)、操作は[runbook](../runbooks/automation.md)を参照する。

## 条件と証拠

| 条件 | 観測結果と経路 | 証拠 |
| --- | --- | --- |
| AC-1 | Issue Scopeの保存とoutboxの間、結果保存の前・後・queue保存後の停止で両方がrollback。受理時は両方を保存 | `TestAutomationPostgres`、`result_acceptance_and_separate_workers` |
| AC-2 | 同じ受理は一意キーで一要求となる。二Workerの同時取得は一所有者。期限切れを同じID・新leaseで回収し、旧所有者の書込みを拒否 | PG並行取得・期限回収・明示適用のreceipt比較 |
| AC-3 | 接続数1のpoolで外部処理を待つ間も別queryが完了。Triage100要求の待機中にHopperを処理。期間件数・固定費用評価・欠測費用で保留 | PGの長い処理fixtureと枠・予算試験 |
| AC-4 | 異なる尺度と優先順を持つ方針から一つを選択。同順位はpolicy_conflict、未設定はpolicy_unsetで待機 | PGの二尺度・三方針fixture、実Ory/MCP |
| AC-5 | 手動P2固定中も最新推奨P0を保存し、採用値を維持。解除で有効な推奨を反映。情報不足・失敗・停止でも採用P0を保持し、状態とstaleを区別 | PGの固定・解除・情報不足・失敗・停止、Webの固定/解除画像 |
| AC-6 | 方針の保存と有効化で過去要求が増えず、明示したページのみ要求を作る。再送は同じ固定結果、継続位置を返す | PGの要求件数、[Web/API結果](assets/issue15-web-api-snapshot.json) |
| AC-7 | 取得前・確定前の主体や現在権限を再確認し、失効時は保留。保存済み結果と優先度を維持 | PGの主体無効化、実Ory/MCPの役割失効・非公開根拠 |
| AC-8 | DBへ要求・担当・状態・再開位置・結果を保存。別Store/Workerから回収し、保存結果は外部処理を再実行せず確定。共通HTTP状態と固定版を取得 | PGの再取得・単一接続Store、HTTP/MCP比較、Web履歴 |
| AC-9 | HTTP/MCPで固定版・優先度・処理状態が一致。read/Context editは更新拒否、triage-editは管理操作拒否、管理操作は明示した目的と現在の管理者権限を確認 | `checkAutomationMCP`、Web実操作。編集者画面では方針編集を提供し、管理設定・有効化ボタンを提供しないことも確認。Windowsの詳細確認は下記 |
| INVAR-1〜3 | Run/pauseを作らず、Hopper/Triageの枠・対話・権限を暗黙に共用しない。完了観測・未受理ログからHopper要求を作らない | PGのsync-report観測fixture、`AcceptResultTx`、実装経路 |

## 実行結果

`pnpm run test:automation`は専用PGでPASS。最新の実行は`TestAutomationPostgres`が3.53秒、packageが4.346秒。transaction停止、CASとreceipt、並行取得、担当期限、再開位置と保存結果、枠・試行・費用予算、固定・解除、欠測・失敗・停止の保持を確認した。別のDB製品・外部LLMは使っていない。

`pnpm run test:mcp`は実Oryと公式SDKでPASS（54.37秒、package 56.253秒）。Triageの固定版・HTTP/MCP再送・明示した編集/管理目的・役割失効・非公開根拠・checkpoint/leaseの非公開に加え、既存のMCP・分類・検索・探索も通過した。最初の実行にはTriageの新fixtureの呼出しが入っていなかったため、その実行だけを本条件の証拠にせず、呼出しを接続した後の実行を採用した。

rootの`check / test / build`と共通Storeの`test:database`はPASS。共通Storeの既存の停止・復元・CAS・API再起動の条件も維持した。UIは10ファイル35試験。Windows releaseアプリのbuildと、専用fixtureの`live_context_ui_prepare`もPASS。既存のCargoのPDB出力名衝突・linker警告は残るがbuildは成功した。

## Webの実操作

実Ory・PG・Bleve・ArcadeDBと1万Context×3版・100Scopeを持つ専用fixture `931fafb68775`を使用した。Webから方針・採用値・推奨・手動固定を取得し、固定解除でP2から有効な推奨P1へ戻した。次に手動P3へ変更し、通常操作で固定されることを確認した。

方針を新版として保存・有効化した前後で対象要求は2件のまま。明示適用したときだけ3件となり、継続位置も表示した。未確定要求の停止と明示再開を行い、履歴へ待機・停止・待機の時刻を取得した。履歴見出しが空になる不具合は同じ画面で再現し、状態・時刻・試行を表示する修正後に再確認した。

- [固定解除](assets/issue15-web-unlocked.jpg)
- [手動固定](assets/issue15-web-fixed.jpg)
- [停止・再開の履歴](assets/issue15-web-history.jpg)
- [方針の新旧版・要求・履歴・採用値のAPI結果](assets/issue15-web-api-snapshot.json)

## Windowsの確認状況と残作業

専用APIの未使用namespaceへDPAPIで検証用認証を保存し、Windowsアプリを起動した。Windows操作ツールの読み取りで、認証済み利用者・mcp-oneの管理者・Triage画面の入口を確認できた。

前面化は`failed to activate captured window`で失敗した。対象を再取得して再試行し、runbookに従いnode_replをresetして再初期化しても同じ結果だった。アプリは応答中で、LockAppや起動モーダルはウィンドウ一覧に無かった。ChromeへWindows操作を切り替えたり、別のUI自動化やOS設定で回避したりしていない。利用者へ一度だけ前面へ出す操作を依頼し、以後の操作はこちらで続ける予定である。

Windowsでの詳細なTriage操作、固定成果物の独立検品、最終head CI、マージ後照合は未完了。未確認をPASSとして扱わず、残作業として保持する。実Hopper発行は#25、LLMのHopperは#27、TriageのLLM評価は#28の対象である。

## 独立検品後のGUI修正

初回の独立検品は `e15d364` を対象にFAILとなった。Issue更新後も、保存済み優先度にある旧対象版をGUIが編集要求へ送っていたため、評価待ち・失敗中の手動変更と固定解除が400で拒否された（B-1、AC-9a）。

修正 `a63f717` では、方針選択時に解決済みの現在の対象版を返し、GUIの編集に使う。保存済み優先度と推奨の根拠は書き換えず、現在版の検証・CAS・現在認可も維持した。PGの同じ経路の回帰試験は修正前に `GUI selection lost current target` でFAIL、修正後はPASS（3.59秒、package 4.346秒）。型・静的検査とWindows release buildも成功した。

専用fixture `798b703de745` のWebで、旧推奨P1・手動採用P2のあるIssueを更新して評価待ちにした。GUIから手動P3の採用・固定と解除がともに成功し、解除後はP3・自動反映・待機のまま、古い推奨P1を採用しなかった。[修正後の画面](assets/issue15-web-current-target-unlocked.jpg)を保存した。HTTP/MCPの比較には現在対象版を返すselectionも含めた。最終buildの共通UI assetは `index-CO_uSBIH.js`。

前のWeb fixtureは正常終了し、[通信の集計](assets/issue15-web-request-summary.json)にはmethod・path・件数だけを保存した。認証情報と本文は含めていない。Windowsアプリの検証用認証は前の専用namespace分を削除し、新しい専用APIの未使用namespaceへDPAPIで準備した。再起動したWindowsアプリも前面化が同じエラーで失敗し、詳細な実操作は未確認のままである。

## Windows実操作の再開と完了（2026-10-16）

利用者が起動済みアプリを前面へ出した後、Computer Useのキー入力でスクロールできた。旧fixtureは期限切れだったため、同じ固定ソース `0358e816` から専用fixture `05902001945e` を起動し直した。未使用API namespaceの検証用認証をDPAPIへ保存して新しいWindowsアプリを起動した。旧アプリとその検証用認証は対象を確認して終了・削除した。通常の開発APIと実GitHub Appは変更していない。

新しい対象の前面化APIは成功を返したが、画像では他ウィンドウが重なっていた。最新画像で見えている対象のタイトルバーを、Computer Useの座標クリックで押し、実際の前面化を確認した。その後は入力欄、ボタン、スクロール、選択メニューをこちらで操作できた。選択メニューの項目クリックはWebView子ウィンドウとの不一致を返したため、対象を再取得し、キーボードで選択・確定した。別のUI自動化やWindows設定の変更はしていない。

Windows版から同じIssueの採用P2・手動固定・推奨P1・待機を取得した。手動P3の保存と固定が成功し、HTTPでもP3・fixed=trueを確認。続けて固定解除し、P3・自動反映・待機のまま、古い推奨P1を採用しないことを画面とHTTPの両方で確認した。方針の現在版 `64LHT7ONBSSOYTZ7L2FEML3GXK` と、対象Issueの処理済み・待機・方針未設定による保留の三要求も同じID・状態・試行回数だった。

- [Windowsの手動固定](assets/issue15-native-fixed.jpg)
- [Windowsの固定解除](assets/issue15-native-unlocked.jpg)
- [Windowsの方針版](assets/issue15-native-policy.jpg)
- [Windowsの処理状態](assets/issue15-native-jobs.jpg)
- [同じfixtureのHTTP保存結果](assets/issue15-native-http-snapshot.json)

これにより、前節まで未確認だったWindowsの詳細なTriage操作を確認できた。製品ソースは変更しておらず、固定成果物の未確認箇所を新しい観測証跡で再判定する。過去の前面化エラーが恒久的に解消したとは扱わず、今回成功した操作と区別する。

今回の `test:mcp:ui` は実操作後の検索エンジン停止・トークン失効試験も含めてPASS（552.79秒、package 554.787秒）。WindowsアプリはAlt+F4で通常終了し、専用fixtureも終了操作で閉じた。今回準備した検証用DPAPIファイルだけを削除した。

## マージ後の照合

PR #48を最終head `e6704707c97ebe90faa59135fd1ffbf176f5253e` の全CI成功後にマージした。CI run `38909134081` はLinux 5分3秒、Windows 5分26秒、GitGuardianが成功。Windows証跡の追加後も製品ソースに変更はなく、独立検品は全AC・INVARをPASSとした。

マージ後main `8d5499cfa72bf4bb44d1fd05646a9b19e1014f68` と最終headのtreeは `7069fe113df7098a092ed9fcffacb183e50ec650` で一致し、`git diff` の差分は0。照合後に#15を手動でCloseし、親#2の該当項目を完了へ更新した。
