# Issue #26の証跡

現在の判定は[current-state.md](current-state.md)へ集約する。この記録は2026-10-18の観測を示す。計画は[Scope revision 1](2026-10-18-issue-26-plan.md)、契約は[ADR 0026](../adr/0026-client-context-selection.md)。基準mainは`e2d6245d2dc2d1bbe4c1691359e50f54a2a54c4e`。

## 対応する確認

| 条件 | 観測・証拠 |
| --- | --- |
| AC-1 | 専用PGの操作、送信意図、原応答、質問、案、採用を固定参照で照合。Run/command/Pause/工程の増加なし。`issue26-clientops-final-local.log` |
| AC-2 | 1万Context×3版から、検索のページ継続と段階取得で四粒度の固定版を選択。別OriginのHTTPS二接続で20要求。保存版のroots/placementsは4/10、4/10、手動3/8、none 0/0で、Run IDは空。`issue26-ui-cross-origin-2.log`、`issue26-client-ui-cross-origin.json`、`issue26-cross-origin-applied.png` |
| AC-3 | none、必須・除外、容量不足、投影未反映、不完全候補、元版の拒否をPG/MCPで確認。通常のページ継続は別扱い。`issue26-page-after.log`、`issue26-pagination-regression-3.log` |
| AC-4 | 実装対話・自己評価の合成マーカーをHostから除外。禁止分類、別Workspace、除外された子をMCPで拒否。独立検品で見つかった初回の禁止固定IDの漏出は、送信予約前の分類検証を追加。失敗/成功は`issue26-inspection-b1-before.log / issue26-inspection-b1-after.log`。所有モデルHTTPへOry/MCP token・cookie・操作handleが届かないことを受信側で検査 |
| AC-5 | 共通保存のHTTP/MCP receipt、元版競合、手動必須をPGで確認。ブラウザでAlphaの自動反映、Betaの採用待ちと明示採用、一項目を外した三項目の手動保存、キー未入力でのnone保存を確認。`issue26-selection-manual.png / issue26-selection-none.png` |
| AC-6 | 送信意図と原応答のACK喪失、同一要求の再送、別クライアントの送信意図、遅い応答、依頼者以外の回答・引継ぎ拒否を確認。再接続だけでは要求を再送しない。`issue26-host-protocol-after.log`、PGの操作試験 |
| AC-7 | `owned-alpha / owned-beta`で同じ四粒度の選択を保存。実モデルの不一致、tool不正、障害、要求・時間・費用上限、手動への復帰を確認。実LLMの意味判断品質を確認したとは扱わない |
| AC-8 | 固定Gatewayのtool・構造化出力・明示モデル・許可fallback・本文記録切替・試行・相関・復旧・fetch送信先を観測。その後の指示によりManifest採用を保留し、[Tauri直接組込み](2026-10-18-issue-26-native-llm-comparison.md)を比較。採用方針は判断待ち、本文ログの初期無効は確定 |
| AC-9 | Windowsの実Oryを使うPKCE・DPAPI・再起動更新・専用resource・ログアウトが成功。登録モデルの実HTTPと暗号化保存、リダイレクト拒否が成功。Webは実ブラウザで専用MCP OAuth、二接続、再読込後のキー消去、保存・採用を確認。費用は操作へ帰属しRunへ加算しない |
| INVAR-1〜3 | クライアントがモデルを呼び、サーバーに代行処理なし。保存・採用からRunは始まらず、モデル・MCPがなくても手動保存できる |

## 実行結果

ログ・PNG・資格情報を除いた所有試験のJSONは、Git管理外の`test-results/`に保存した。実プロバイダ資格情報や実Workspaceの本文は使っていない。各DB試験は専用Docker projectを終了時に削除した。

| コマンド・試験 | 結果 |
| --- | --- |
| `pnpm run check` | 成功。`issue26-root-check-2.log` |
| `pnpm run test` | 成功。共通UI 63件、Goの対象package。`issue26-root-test.log` |
| `pnpm run build` | 成功。`issue26-root-build.log`。既存構成のbundleサイズ警告あり |
| `pnpm run test:clientops` | 成功。`issue26-clientops-final-local.log` |
| `pnpm run test:mcp` | 成功、100.35秒。`issue26-pagination-regression-3.log` |
| `pnpm run test:usage` | 成功、package 7.775秒。`issue26-usage-regression.log` |
| `pnpm run test:workflows` | 成功、package 8.978秒。`issue26-workflow-regression.log` |
| `pnpm run test:hopper` | 成功、package 12.305秒。`issue26-hopper-regression.log` |
| `pnpm run check:desktop` | 成功、21.32秒。`issue26-native-current-check.log` |
| `pnpm run test:desktop` | Windows上で23件成功・10件ignore。当該モデルHTTP・暗号化保存の3件は実行済み。`issue26-model-native-current.log` |
| 所有Oryによる追加のnative MCP試験 | 1件成功、1.14秒。`issue26-native-mcp-ory.log`。通常の開発アカウント・既存の秘密ファイル・実GUI起動は使わない |
| 所有Gatewayの追加probe | 成功。`issue26-manifest-probe-recording.log`、`issue26-manifest-probe-final.json` |
| 別Origin HTTPSの共通画面 | 成功、313.66秒（画面操作の待機を含む）。`issue26-ui-cross-origin-2.log`。Webとモデルは別port、二モデルの20要求でcookie・MCP header受信0 |
| Windows上の組込みSDK候補 | rig-core/rig-agent 0.44.0とgenai 0.7.0-rc.8で、それぞれ2モデル要求・1tool往復が成功。`issue26-sdk-probes.log`。製品への組込みや実OAuthの成功には数えない |

## rigの直接組込み

比較後、ユーザーがrig-core＋rig-agentを選択した。Tauriへ固定source `c73622dfca02bfe55d29a6a0612e181119cab9b8`を組み込み、直接HTTPだけだったnative経路を置き換えた。WebのHTTP経路は維持した。SDKのCLI/harness/Bevyは依存に含めない。

共通Hostの送信意図一件に対してnativeのAgentRunを一モデル呼出しだけ進め、MCPの実行と次の共有予約はHostへ戻す。SDK内部のcheckpointを共有形式へ保存しない。原JSON、SSE、モデル、usage、HTTP要求ID、送信先、SDK版を残す。OAuth応答はモデルcaptureの対象から除外する。typed messageの再取得には本文を含むchoiceも必要であり、headerだけを保存して次のtool応答が拒否される初期実装を修正した（`issue26-rig-loop-diagnostic.log`、修正後の往復試験）。

SSEのincomplete応答で、有効なJSON本文があってもlengthをstopへ変えない。変更前は`issue26-rig-partial-before.log`でstopとなる失敗を残し、修正後は既存Goの不完全応答拒否へ渡す。nativeの内部messageをWebのHTTPへ引き継がないことも、`issue26-web-native-envelope-before.log / issue26-web-native-envelope-after.log`で照合した。

WindowsのDPAPI TokenStoreはAPI・利用者・接続で保存先を分け、SDKのFileTokenStoreはlockだけに使う。APIキーとOAuth tokenをWeb、共有記録、Ory/MCP、CLIへ渡さない。モデル呼出し中は非対話の更新だけを許可し、本人の明示ログインを別操作にした。ChatGPTの更新・失効、Copilotのsession交換・再利用・端末からの削除は所有プロトコルで確認した。実契約での本人認証・モデル利用は未確認であり、新しいOAuth接続と最大2回の合成要求の許可をユーザーへ確認中。

| 組込み後の確認 | 結果 |
| --- | --- |
| nativeの関連試験 | Windowsで10件成功。SDKの二要求/一tool往復、SSEの元応答・usage・途中終了、OAuth capture除外、DPAPI分離、非対話更新、Copilot交換、登録先/リダイレクト拒否。`issue26-rig-native-final-local.log` |
| native全体 | 30件成功・12件ignore。`issue26-rig-desktop-all.log`。新しい実契約trialはignoreし、本人の許可なく起動しない |
| Host/接続 | 13件成功。`issue26-rig-host-web-tests.log` |
| 共通UI全体 | 64件成功。`issue26-rig-client-all.log` |
| root check/build | 成功。`issue26-rig-root-check.log / issue26-rig-root-build.log`。既存のbundleサイズ警告は継続 |
| 追加依存 | 全targetで20件。初期Windowsのnormal/build graphは18件、MIT選択が可能。11の通知を`apps/client/public/third-party-rig.txt`へ同梱。build後のassetの存在を確認。`issue26-rig-added-licenses.json / issue26-rig-windows-dependencies.txt` |

実契約trialは`live_subscription_two_request_trial`へ分離した。新しい接続先を明示し、合成データのみ、最大2回、APIキー課金へのfallbackなし。完了・失敗のいずれでも試験用認証を削除し、確認できた失効結果だけを保存する。通常のテストや起動からは実行しない。

## 失敗と修正の区別

ブラウザでMCP初期化がHTTP 400となり、モデル要求は0件だった。採用したSDK/protocolの要求を照合し、stateless discovery、要求メタデータ、HTTP method/nameヘッダを追加した。`issue26-mcp-init-before.png`と、その後の選択・保存を対応させる。

モデル応答の保存だけで検索が`projection_pending`となる不具合と、通常の次ページを不完全と判定する不具合を修正した。検索・グラフの索引付き変更範囲と継続を検証し、実ブラウザの四粒度選択が保存まで進んだ。手動none保存が以前のincludeを送って拒否される不具合も、明示noneでは空入力を送るよう修正した。

候補Gatewayの未知モデルが必ずHTTPエラーになるという初期fixtureの誤り、distroless image内の実行コマンド指定の誤り、未反映の大量グラフを継続試験の前提にしたfixtureの誤りは、製品修正と区別する。グラフの継続試験は大量データ投入前の固定した投影で行う。

共通UI試験で既存の同期storeの5秒timeoutが発生した実行は残した。timeout値を変えず、単独workerでは63件すべて成功し、その後の標準`pnpm run test`も63件成功した。並行して重いDB試験を動かした実行では、大量検索が5秒以内に完了せず失敗した。成功実行と混ぜず、同時負荷のない最終試験の結果を別記する。

## 残作業

固定commit `2e1f36618b6095e37c3f7653bc6b46b04e24edc1`の独立検品で二件のblockerを確認した。禁止された手動必須ContextのID/版が初回モデル入力へ入る経路と、提案の明示採用が共通入力へ反映されない経路である。

前者は送信意図の確定前に固定includeも操作の送信分類で検証し、手動保存は可能なまま、禁止参照のモデル要求数が0であることをPGで確認した。後者は明示採用でも共通の反映処理を呼ぶよう修正した。二項目の案を空の入力へ採用すると、修正前は0項目のまま、修正後は2項目へ変わった（`issue26-adopt-before.png / issue26-adopt-after.png`）。

採用保存中の手動編集は、所有DBのWorkspace streamを30秒だけロックして確認した。保存待ち中に入力をnoneへ変え、保存後にもnoneを保ち、差分確認の通知を表示した（`issue26-adopt-edit-lock.log / issue26-adopt-manual-edit.png`）。この確認に新しい製品用の遅延機構は追加していない。

残作業は修正差分の独立再検品、組込みSDK/接続方式のユーザー判断、その判断に必要な実装・検証、最終headのCI、マージ後のtree照合、手動Close。#33へ移管したS3スリープと人の初見操作は未確認のまま維持し、今回の完了へ算入しない。
