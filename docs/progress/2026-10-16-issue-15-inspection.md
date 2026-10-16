# Issue #15 独立検品

## 対象と入力

- 対象commit: `e15d36416eeb415b8f0226840f0236ce0b23c614`。
- 基準main: `cdf6c14ad6a97c50db1e385a3714c84fc3102f29`。
- 関連Issue: #15、Scope revision 1、区分C。
- 検品者・実施日: 独立検品用の別コンテキスト、2026-10-16。
- 目的: 受理と永続要求、Worker、版付き方針と優先度、共通の認可・公開境界を、[計画](2026-10-16-issue-15-plan.md)の末端fixtureまで確認する。
- 対象外: 実Hopper発行 #25、LLM Hopper #27、LLM Triage・外部同期 #28。結果受理は `AcceptResultTx` と同一transactionのfixtureで判定し、未受理の `sync-report` を受理イベントへ昇格しない。
- 入力: Issue本文、AGENTS.md・AGENTS.local.md、開発・Issue手順、検品雛形、設計書§6.12・6.13・7.14〜7.16・12.6・12.7、ADR 0015、計画、[実装側の証跡](2026-10-16-issue-15-evidence.md)、`assets/issue15-*`、対象差分、コードとテスト。
- ユーザー確定事項: 編集可能なP0〜P3初期テンプレート、通常の手動変更の固定、有効化・主体権限・枠・予算の管理者による管理。
- 渡さない情報: 実装セッションの会話履歴と自己評価。証跡の記載を合否として継承せず、コード・観測・独立実行と照合した。
- 環境: Windows、PowerShell、リポジトリ同梱Go、専用Docker Compose。標準8080のプロセス、実GitHub App・鍵、他アプリは操作していない。
- CodeGraph: `.codegraph/` を確認し、コード探索・読取りに `codegraph_explore` / `codegraph_node` を先に使用した。
- 固定性: 固定試験の開始時と完了時のHEADは対象commitと一致し、作業treeにも変更がなかった。検品者は製品ソース・テスト・Gitを変更せず、本記録だけを作成した。追加試験はOS一時ディレクトリのGo overlayで行った。その後に実装担当が開始したB-1修正は本検品へ含めない。

## 証拠の略号

| 略号 | 証拠 |
| --- | --- |
| PG | 独立実行した `pnpm run test:automation`、`internal/automation/postgres_test.go` |
| MCP | 独立実行した `pnpm run test:mcp`、`checkAutomationMCP` と既存の認可・none/exclude fixture |
| 追加試験 | 固定テストへ一時的に追加した `independent_acceptance_edges`。8並列再送、二Scope・二尺度、古い入力、主体の役割失効、別Storeからの取得、参照前後の件数、GUI payloadの拒否を確認 |
| Web | `issue15-web-unlocked.jpg`、`issue15-web-fixed.jpg`、`issue15-web-history.jpg` と `issue15-web-api-snapshot.json`。画像を目視し、APIの固定版・値・要求・履歴と照合 |
| 経路 | 対象差分と `automation/{service,worker,http,model}.go`、`mcpapi/automation.go`、`automation.tsx`、Storeの同一transaction経路 |

## 条件ごとの結果

計画の各行を末端の観測対象へ分けた。PASSは下記の範囲に限る。Windowsの詳細操作は別行で未確認とした。

| 条件・末端fixture | 確認した経路・期待結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 結果保存前 | owning domainの保存前停止で結果・Hopper要求が両方なし | PG `result_acceptance_and_separate_workers` 380〜419行 | PASS | 実Run domainの接続は対象外 |
| AC-1 結果保存後 | 結果だけを書いた後の停止でも両方なし | 同上 | PASS | なし |
| AC-1 要求保存後・commit前 | queue追加後の停止で両方なし、正常受理で結果と1要求 | PG 394〜428行、`AcceptResultTx` | PASS | なし |
| AC-1 Issue更新 | `Store.Accepted` が同じtxでTriage要求を書き、queue後の停止でScope版・要求件数を維持 | PG 108〜127行、Store 214〜240行 | PASS | なし |
| AC-2a 同一要求の並行再送 | 同じ更新を8並列に送信し、同一版・1要求・1receipt・1更新履歴へ収束 | 追加試験、queueの複合UNIQUE制約 | PASS | なし |
| AC-2b 二Worker同時取得 | 同時取得の所有者は1つ。処理枠も重複しない | PG `parallel_claim`、128〜135行 | PASS | なし |
| AC-2b 期限切れ・再起動・古い担当 | 新Workerが同じIDと新leaseで回収し、旧leaseのcheckpoint書込みを拒否 | PG 320〜337行、`sameLease` | PASS | なし |
| AC-3a 外部処理中のtx | 接続数1でexecutor待機中も別queryが完了 | PG 449〜483行、`RunOne` のClaim後のexecutor呼出し | PASS | なし |
| AC-3b 枠の独立 | 各枠1、Triage100要求と待機中executorがあってもHopperが完了 | PG 443〜492行 | PASS | 性能SLAへの一般化はしない |
| AC-3b 期限・再試行・停止 | 種別ごとのControl、担当期限、最大試行後のfailed、stop後の旧担当拒否、明示resumeと履歴を確認 | PG 198〜235・285〜298・320〜337行、worker 155〜180・221〜273行、Web履歴 | PASS | なし |
| AC-3b 件数・費用・欠測 | 期間試行上限、固定費用評価による上限、欠測による保留を区別 | PG 494〜587行、worker `budget` | PASS | 実請求額の厳密な上限は対象外 |
| AC-4 二Scope・二尺度・固定版 | 親MilestoneとIssue固有の方針を登録。P0〜P3とnow/laterの尺度から優先順で1版を選び、無効化後は親方針へ戻る | 追加試験、`selectPolicy` | PASS | なし |
| AC-4 未設定・同順位競合 | `policy_unset` / `policy_conflict` を返し、最低優先度を生成しない | PG 87〜107・238〜256行 | PASS | なし |
| AC-5 採用値と最新推奨 | P2固定中の新推奨P0を保存し、採用値はP2を維持 | PG 152〜169行、Web固定画像 | PASS | なし |
| AC-5 手動変更・解除・履歴 | 正しい現在対象版を渡すservice操作では固定と解除を保存し、有効な推奨を反映。版付き記録と理由を保持 | PG 152〜178行、追加試験、Web解除画像、`save` と `prepare` | PASS | GUIの古い対象版はB-1 |
| AC-5 古い入力の推奨 | 処理中にIssueを更新すると旧入力の結果を採用しない。正しい現在版による解除でも旧推奨を反映しない | 追加試験、worker `authority`、service 263〜276行 | PASS | なし |
| AC-5 未評価・欠測・失敗・再評価待ち | 欠測・失敗・停止・古い版を優先度値と別状態にし、既存P0/P2を維持 | PG 180〜235行、追加試験、`Priority` | PASS | なし |
| AC-6 保存・有効化 | 保存・有効化だけでは過去要求が増えない。保存版と有効版は別記録 | PG 92〜101行、Web/APIで2件のまま、経路 | PASS | なし |
| AC-6 明示ページ・再送 | limit付きページだけ要求し、継続位置と固定receiptを返す。再送は増加しない | PG 257〜266行、Web/APIの明示適用後3件、`ApplyPast` | PASS | なし |
| AC-7 取得前の失効 | 管理者をviewerへ変更後のClaimは取得せず保留 | 追加試験、`authority` | PASS | なし |
| AC-7 処理中・確定時の失効 | 主体無効化または現在の管理者役割失効後のFinishは採用値を維持し `authority_revoked` で保留 | PG 267〜283行、追加試験 | PASS | なし |
| AC-7 再送前・履歴維持 | 更新入口・receipt読取りより前に現在権限を再確認。MCPの役割失効後は更新拒否。保存結果・優先度・履歴は残る | Store 183〜199行、service 140〜151行、MCP、追加試験 | PASS | なし |
| AC-8 再起動後の要求・担当・版・保留理由 | 新Workerで回収。別Storeを開き、Priorityの固定版と値、Jobの担当と保留理由が同一であることを照合。保存結果の再開はexecutorを再実行しない | PG 285〜298・320〜337・449〜455行、追加試験、HTTP/MCP共通Query比較 | PASS | OSプロセス強制終了を別途試す条件へは拡張しない |
| AC-9a GUI/HTTP/MCPの固定版・通常編集 | Webの固定/解除・保存/有効化・停止/再開、HTTP/MCPの同一固定版・再送・優先度・要求応答を照合 | Web、MCP 50〜135行 | PASS | 古い対象版の経路は次行 |
| AC-9a 更新後・再評価待ちのGUI編集 | 保存済み優先度のあるIssueを更新した後、画面の `current.target` を使う手動変更と固定解除がHTTP 400になる | 追加試験のGUI payload再現、B-1 | FAIL | B-1修正後のWeb実操作が必要 |
| AC-9a Windows GUI | release build・認証済みAX読取り・画面入口までは提供証拠あり。詳細なTriage操作は実施できていない | 証跡のWindows確認状況、引継ぎ観測 | INCONCLUSIVE | 通常の前面化・対象再取得・セッションreset・観測されたRaiseが `failed to activate captured window`。利用者の一度の前面化後に操作を続ける必要あり |
| AC-9b Workspace・非公開根拠 | 他Workspace拒否、非公開対象を参照する優先度のget拒否・jobsから除外 | PG 317〜318行、MCP 136〜151行、依存参照のReadFilterSQL | PASS | なし |
| AC-9b read/edit・none/exclude・管理境界 | read/Context editからTriage更新拒否、triage-editから有効化拒否、管理者・明示triage-manageを要求。none/excludeは既存の共通認可を維持 | PG 300〜315行、MCPと共通認可fixture、access差分 | PASS | なし |
| AC-9b 秘密値・操作範囲外 | 参照依存の現在認可を確認。公開jobsはlease/checkpoint本文を除外し、historyも本文を返さない | MCP 123〜146行、http 62〜78行、Read/checkRefs | PASS | サーバーLLMの鍵は今回追加していない |
| AC-9b 参照だけの副作用 | 追加試験のheads/records/jobs/priority取得前後で要求件数が不変。参照経路にexecutor・発行・Run作成呼出しがない | 追加試験、Query/Read/Priority・MCP経路 | PASS | なし |
| INVAR-1 | automation記録と内部Executor契約を使い、Run/pauseを作らない | 経路、PG/MCP | PASS | なし |
| INVAR-2 | kind別Control・予約・予算・権限を使い、対話履歴を追加・共用しない | PGの別枠fixture、worker/control、ADR 0015 | PASS | なし |
| INVAR-3 | `sync-report` の完了観測はHopper要求を増やさない。未同期・未受理ログを受理扱いしない | PG 430〜441行、`accepted` / `AcceptResultTx` | PASS | 実Run受理は後続Domain |

## 指摘

| 指摘 | 関係する条件 | 到達経路と具体的な影響 | 根拠 | 分類 |
| --- | --- | --- | --- | --- |
| B-1 / P1: Issue更新後にGUIから優先度を手動変更・固定解除できない | AC-9a。AC-5で想定する既存値保持・再評価待ち中の手動操作への入口 | 優先度を保存したIssueのScopeを更新し、評価が未完了の状態で「現在の優先度・方針を取得」後に手動変更または解除する。Priority応答のtargetが旧版のままで、GUIが旧版を送るため400となる。評価待ち・失敗・停止中も同じで、既存値は保持されるが手動判断を更新できない | `http.go` 134〜157行は保存値を読んだ後もtargetを現在版へ更新しない。`automation.tsx` 661〜684行は `current.target` を送信。`service.go` 263〜265行が現在版不一致を `ErrInvalid` にする。下記の独立再現 | blocker |

B-1の再現では、Scope更新前の優先度を保存し、Scope更新後に `Priority` 応答からGUIと同じpayloadを作った。`Unlock=false` と `Unlock=true` の両方が `invalid_automation` になり、応答のDataはなかった。HTTPの `reply` はこれを400へ変換し、GUIは汎用の失敗表示になる。

```text
GUI blocker reproduced: unlock=false
  displayed-target=IKQOVDEECREFYOH46HXN3H24Z6
  current-target=BM5RIOSALC42XDMPW7IPA753QF rejected=invalid_automation
GUI blocker reproduced: unlock=true
  displayed-target=IKQOVDEECREFYOH46HXN3H24Z6
  current-target=BM5RIOSALC42XDMPW7IPA753QF rejected=invalid_automation
```

同じ試験で、正しい現在対象版を指定したserviceの解除は成功し、旧推奨を反映せず採用P2を維持した。既存の版検証を弱めず、画面が現在の操作対象版と古い推奨の評価対象版を区別して送れることを再確認する必要がある。新しい利用場面や入力条件は追加していない。

## 実行結果

| 検証 | 結果 |
| --- | --- |
| 固定ソースの `pnpm run test:automation` | PASS。`TestAutomationPostgres` 3.57秒、package 4.366秒 |
| 固定ソースの `pnpm run test:mcp` | PASS。`TestMCPIntegration` 56.23秒、package 58.023秒。Triage fixture呼出しのログあり |
| Go overlayによる追加試験 | 通常の追加条件はPASS。B-1の拒否を期待する再現assertionも成立。最終実行は4.02秒、package 4.784秒。このテスト成功をB-1の受入成功としては扱わない |
| Webの画像・API照合 | 固定解除P1、手動固定P3、停止/再開の履歴と固定版を確認 |
| Windows詳細操作 | 未実行。前面化の失敗が継続している提供観測を保持 |

各専用Composeは試験後に自身のラベルを検証して削除された。通常のroot・desktop・共通Store回帰検証は提供証跡を確認したが、この検品で再実行したとは扱っていない。

追加試験は一時ファイル `C:/Users/kgm11/AppData/Local/Temp/koujou-issue15-review-e328c456c1bb432a81332298bab0ec3b/postgres_test.go` を元のテストpathへGo overlayで割り当て、`GOFLAGS=-overlay=<同ディレクトリのoverlay.json>` を設定した `pnpm run test:automation` で実行した。追加後の一時テストのSHA256は `1AC1C25FE6A2FD4E6BF39F1113EBAA6E050E36F4C36792166AE9BACD804D12E0`。一時ファイルの存続は成果物の契約に含めず、再現条件と観測は本記録に残した。

## 判定

- 総合判定: **FAIL**。
- 製品のblocker: B-1の1件。AC-9aのGUI編集経路が未達。
- 未確認: Windowsの詳細なTriage実操作。build・認証済みAX読取りだけではPASSにしない。
- 修正後の再確認: B-1に関係する現在の対象版・古い推奨・手動変更・固定解除の差分とWeb実操作。正しい現在版では編集でき、旧入力の推奨は採用せず、CAS・手動固定・履歴を維持することを確認する。Windows操作を終えてAC-9aを再判定する。
- 最終head CIとマージ後照合は本検品の対象commit後の作業であり、本記録から成功とは判定しない。

## B-1の差分検品（2026-10-16）

前節までのFAILは `e15d36416eeb415b8f0226840f0236ce0b23c614` の記録である。B-1の修正対象を `a63f7172cce5191d584833931606b7fc3d0170cf` に固定し、前回からの製品・試験4ファイルと本記録の差分を確認した。検品者は製品ソースを変更していない。B-1、旧推奨・保存版、CAS・再送、現在認可への影響に範囲を限定した。

`Query(selection)` は、既に解決・認可確認した現在対象版を `target` として返す。GUIはその参照を手動変更・解除へ渡す。保存済みPriorityの対象版と最新推奨の評価対象版は書き換えていない。既存の現在版確認、CAS、権限確認も維持されている。

| 影響する条件 | 差分の確認と結果 | 判定 |
| --- | --- | --- |
| B-1 / AC-9a 評価待ち中のGUI編集 | 現在版のselectionを取得し、手動P3へ変更して解除。採用値P3、固定解除、待機を保持し、旧推奨P1を採用しない。独立PGの同じpayload経路と提供されたWeb実操作・画像を照合 | PASS |
| B-1 / AC-9a 失敗中のGUI編集 | 独立PGでexecutor失敗後にselectionから現在対象版を取得。手動P2へ変更して解除でき、旧推奨P1を採用しない | PASS |
| AC-5 古い推奨と保存版の意味 | 旧推奨P1の対象版を最初の版のまま維持。Issue更新後の取得では保存済みTargetが旧版、selectionだけが現在版。手動P3/P2と解除後も推薦値と推薦対象版を保持 | PASS |
| AC-2 / AC-9 更新再送とCAS | 同じ手動更新の再送は同一固定参照へ収束。古い優先度版を使う別操作は拒否し、Dataを返さない | PASS |
| AC-7 / AC-9 現在認可・公開境界 | 同一receiptがある手動更新も、管理者をviewerへ変更した後の再送は拒否。noneと非公開対象のselectionはDataを返さず拒否 | PASS |
| AC-9 HTTP/MCP共通応答 | selectionを含むHTTP/MCP比較fixtureを確認。旧い推奨を持つIssueを再更新した状態で同じ現在対象版を返す | PASS |
| AC-9 Windows詳細操作 | 同buildの新しいウィンドウでも前面化失敗が継続している提供観測あり。本人の前面化待ちで詳細操作は未実施 | INCONCLUSIVE |

独立追加試験 `independent_b1_current_target` は、旧推奨と異なる手動値を使って旧推奨の誤採用を識別した。評価待ちではP3、失敗中ではP2を採用し、いずれも解除後にP1へ戻らなかった。

```text
B-1 waiting: current selection edited/unlocked P3,
  retained distinct old recommendation P1,
  stale CAS/replay/role/none/private boundaries passed
B-1 failed: current selection edited/unlocked P2,
  retained distinct old recommendation P1,
  stale CAS/replay/role/none/private boundaries passed
```

固定ソースの独立 `pnpm run test:automation` はPASS（3.74秒、package 4.605秒）。同じ固定ソースにOS一時ディレクトリのGo overlayで追加試験を重ねた実行もPASS（4.27秒、package 5.071秒）。追加試験ファイルは `C:/Users/kgm11/AppData/Local/Temp/koujou-issue15-b1-review-ac87221ea7fd41a68d0b167d32f206d2/postgres_test.go`、SHA256は `DBFEA25647FF9E80E9A809BCCA9F12BEC8ACD35E8AFCDCA79B5D24708E68E535`。

独立 `pnpm run test:mcp` もPASS（`TestMCPIntegration` 58.59秒、package 60.387秒）。selectionを追加したHTTP/MCP比較、現在役割・非公開根拠・none/exclude・CAS/再送を含む共通fixtureを通過した。各試験の専用Composeは自身の資源だけを終了・削除した。yomiyasuの静的検査は入力一覧の列挙比率のnoticeのみで、役割が明確な一覧として維持した。`git diff --check` は成功した。

Webは専用fixture `798b703de745`、build `index-CO_uSBIH.js` で実装担当が再操作した。旧推奨P1・保存済み手動P2・新Issue版の待機状態からP3へ変更して固定し、続けて解除する両操作が成功した。[解除後の画像](assets/issue15-web-current-target-unlocked.jpg)を検品者が目視し、P3・自動反映・待機・最新推奨P1を確認した。画像は固定ソースでの操作後に追加された観測成果物として扱う。

現在のB-1判定は **解消**。差分範囲に残る製品blockerは0件。前回の他条件のPASSを維持する。Windows詳細操作が未確認のため、全体の現在判定は **INCONCLUSIVE** とし、Issue完了やWindows適合を成功扱いしない。最終head CI・マージ後照合は別途必要である。
