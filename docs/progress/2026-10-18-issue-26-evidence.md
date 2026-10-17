# Issue #26の証跡

現在の判定は[current-state.md](current-state.md)へ集約する。この記録は2026-10-18の観測を示す。計画は[Scope revision 1](2026-10-18-issue-26-plan.md)、契約は[ADR 0026](../adr/0026-client-context-selection.md)。基準mainは`e2d6245d2dc2d1bbe4c1691359e50f54a2a54c4e`。

## 対応する確認

| 条件 | 観測・証拠 |
| --- | --- |
| AC-1 | 専用PGの操作、送信意図、原応答、質問、案、採用を固定参照で照合。Run/command/Pause/工程の増加なし。`issue26-clientops-final-local.log` |
| AC-2 | 1万Context×3版から、検索のページ継続と段階取得で四粒度の固定版を選択。所有する二モデルのHTTPと共通UIで保存を確認。`issue26-ui-3.log`、`issue26-client-ui.json`、`issue26-selection-applied.png`。別OriginのHTTPS接続と最終Manifestの対照は追記予定 |
| AC-3 | none、必須・除外、容量不足、投影未反映、不完全候補、元版の拒否をPG/MCPで確認。通常のページ継続は別扱い。`issue26-page-after.log`、`issue26-pagination-regression-3.log` |
| AC-4 | 実装対話・自己評価の合成マーカーをHostから除外。禁止分類、別Workspace、除外された子をMCPで拒否。所有モデルHTTPへOry/MCP token・cookie・操作handleが届かないことを受信側で検査 |
| AC-5 | 共通保存のHTTP/MCP receipt、元版競合、手動必須をPGで確認。ブラウザでAlphaの自動反映、Betaの採用待ちと明示採用、一項目を外した三項目の手動保存、キー未入力でのnone保存を確認。`issue26-selection-manual.png / issue26-selection-none.png` |
| AC-6 | 送信意図と原応答のACK喪失、同一要求の再送、別クライアントの送信意図、遅い応答、依頼者以外の回答・引継ぎ拒否を確認。再接続だけでは要求を再送しない。`issue26-host-protocol-after.log`、PGの操作試験 |
| AC-7 | `owned-alpha / owned-beta`で同じ四粒度の選択を保存。実モデルの不一致、tool不正、障害、要求・時間・費用上限、手動への復帰を確認。実LLMの意味判断品質を確認したとは扱わない |
| AC-8 | 固定Gatewayのtool・構造化出力・明示モデル・許可fallback・本文記録切替・試行・相関・復旧・fetch送信先を観測。依存と配布の調査を[Gateway評価](2026-10-18-issue-26-gateway-probe.md)へ記録。初期提供方式と本文ログはユーザー判断待ち |
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

## 失敗と修正の区別

ブラウザでMCP初期化がHTTP 400となり、モデル要求は0件だった。採用したSDK/protocolの要求を照合し、stateless discovery、要求メタデータ、HTTP method/nameヘッダを追加した。`issue26-mcp-init-before.png`と、その後の選択・保存を対応させる。

モデル応答の保存だけで検索が`projection_pending`となる不具合と、通常の次ページを不完全と判定する不具合を修正した。検索・グラフの索引付き変更範囲と継続を検証し、実ブラウザの四粒度選択が保存まで進んだ。手動none保存が以前のincludeを送って拒否される不具合も、明示noneでは空入力を送るよう修正した。

候補Gatewayの未知モデルが必ずHTTPエラーになるという初期fixtureの誤り、distroless image内の実行コマンド指定の誤り、未反映の大量グラフを継続試験の前提にしたfixtureの誤りは、製品修正と区別する。グラフの継続試験は大量データ投入前の固定した投影で行う。

共通UI試験で既存の同期storeの5秒timeoutが発生した実行は残した。timeout値を変えず、単独workerでは63件すべて成功し、その後の標準`pnpm run test`も63件成功した。並行して重いDB試験を動かした実行では、大量検索が5秒以内に完了せず失敗した。成功実行と混ぜず、同時負荷のない最終試験の結果を別記する。

## 残作業

Gatewayのユーザー判断、別OriginのHTTPS画面試験の記録、固定成果物の独立検品、最終headのCI、マージ後のtree照合、手動Close。#33へ移管したS3スリープと人の初見操作は未確認のまま維持し、今回の完了へ算入しない。
