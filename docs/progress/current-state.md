# 現在状態

更新日: 2026-10-16（JST）

開発環境の構築を終え、[親Issue #2](https://github.com/KingYoSun/koujou-san/issues/2)の実装を#3から番号順に進めている。製品設計は[設計書](../../koujou-san-design-doc.md)、環境構築の証跡は[2026-10-15の記録](2026-10-15-development-environment.md)を参照。

## 環境構築の受入条件

| 条件        | 期待結果                               | 確認方法                                      |
| ----------- | -------------------------------------- | --------------------------------------------- |
| AC-ENV-1    | Issueや直接の指示から実装を開始できる  | 指示書、手順、Issue／PR様式の参照と記述を照合 |
| AC-ENV-2    | 固定依存を導入して検証できる           | setup／doctor／check／test／buildとnative検証 |
| AC-ENV-3    | 共通UIとGo APIの起動を確認できる       | healthとWebの応答、nativeの起動確認           |
| AC-ENV-4    | 標準サービスの開発用構成が起動する     | Compose設定、migration、health・接続確認      |
| INVAR-ENV-1 | 既存の設計書とkukuriを変更しない       | Git差分・対象pathの確認                       |
| INVAR-ENV-2 | 個人設定・秘密値・既存権限を複製しない | ignoreと新規.env生成、設定の確認              |

## Issue #3の完了

[PR #35](https://github.com/KingYoSun/koujou-san/pull/35)をCI全成功後にマージした。main `c478dd901a3414d9ce824ba9586fe98fa997ae84` と検証対象のtree差分は0。全AC・独立検品・マージ後照合が揃い、#3をCloseした。契約と採用OSは[ADR 0002](../adr/0002-shared-api-contract.md)、先行結果は[検証記録](2026-10-15-issue-3-evidence.md)、検品は[独立検品記録](2026-10-15-issue-3-inspection.md)を参照する。

## Issue #4の判定

状態: Complete。[PR #36](https://github.com/KingYoSun/koujou-san/pull/36)をCI全成功後にマージした。main `89982324415fecbe5339874e198f5058ca5d07ac` と最新検証対象のtree差分は0。AC-1〜6、維持条件、[独立検品](2026-10-15-issue-4-inspection.md)、CI、マージ後照合が揃い、#4をCloseした。[ADR 0003](../adr/0003-postgresql-persistence.md)、[検証記録](2026-10-15-issue-4-evidence.md)、[保存と復旧](../runbooks/persistence.md)を参照する。

## Issue #5の判定

状態: Complete。Scope revision 1。[PR #37](https://github.com/KingYoSun/koujou-san/pull/37)を全CI成功後にマージした。main `0eeee997d5365ad9db1b7af35452102b2f3b12fa`と検品済みheadのtree差分は0で、AC-1〜6・INVAR-1〜4、[独立検品](2026-10-15-issue-5-inspection.md)、マージ後照合が揃い、#5をCloseした。[検証記録](2026-10-15-issue-5-evidence.md)を参照する。Windows経由のChromeのURL判定は未解消だが、Chrome拡張とWindowsアプリの正規経路で画面検証を進められることを確認した。[操作手順](../runbooks/computer-use.md)を参照する。

## Issue #6の判定

状態: Complete。Scope revision 1。[PR #38](https://github.com/KingYoSun/koujou-san/pull/38)を全CI成功後にマージした。main `491ecd9c6a6640f0f0d857a21069c848019eaf23`と検証済みheadのtree差分は0。実PG・実Ory・Web/Tauri・Windows DPAPI試験、[独立検品](2026-10-15-issue-6-inspection.md)、マージ後照合が揃い、#6をCloseした。[検証記録](2026-10-15-issue-6-evidence.md)を参照する。

## Issue #7の判定

状態: Complete。Scope revision 1。[PR #39](https://github.com/KingYoSun/koujou-san/pull/39)を全CI成功後にマージした。main `70996e6669d8f85ef23b9e0f71337f6d74111e02`と検証済みheadのtree差分は0。AC-1〜8、INVAR-1〜5、[独立検品](2026-10-15-issue-7-inspection.md)、マージ後照合が揃い、#7をCloseした。[検証記録](2026-10-15-issue-7-evidence.md)を参照する。

## Issue #8の判定

状態: Complete。Scope revision 1。[PR #40](https://github.com/KingYoSun/koujou-san/pull/40)を全CI成功後にマージした。main `a744639ee4aa7084e20a031fb114e4dd072c385b`と検証済みheadのtree差分は0。[検証記録](2026-10-15-issue-8-evidence.md)の実PG・2アカウントWeb/Tauri、[独立検品](2026-10-15-issue-8-inspection.md)、マージ後照合が揃い、#8をCloseした。

## Issue #9の判定

状態: Complete。Scope revision 1。[PR #41](https://github.com/KingYoSun/koujou-san/pull/41)を全CI成功後にマージした。main `730f20ff9ae3c0bf4dcf2dc1d12270796c4a93e3`と検証済みheadのtree差分は0。[検証記録](2026-10-15-issue-9-evidence.md)、[独立検品](2026-10-15-issue-9-inspection.md)、合意負荷・マージ後照合が揃い、#9をCloseした。

## Issue #10の判定

状態: Complete。Scope revision 1。[PR #42](https://github.com/KingYoSun/koujou-san/pull/42)を全CI成功後にマージした。main `1f9acedb7cd091ef9da10c5bb8c4fd665b991305`と検証済みheadのtree差分は0。[検証記録](2026-10-15-issue-10-evidence.md)、[独立検品](2026-10-15-issue-10-inspection.md)、独立負荷再実行とマージ後照合が揃い、#10をCloseした。

## Issue #11の判定

状態: Complete。Scope revision 1。[PR #43](https://github.com/KingYoSun/koujou-san/pull/43)を全CI成功後にマージした。main `4d3f16dc42dfa85d2432e82067b4f1768b553f40`と検証済みheadのtree差分は0。AC-1〜6・INVAR-1〜5、[検証記録](2026-10-16-issue-11-evidence.md)、[独立検品](2026-10-16-issue-11-inspection.md)、マージ後照合が揃い、#11をCloseした。

## Issue #12の判定

状態: Complete。Scope revision 1。[PR #44](https://github.com/KingYoSun/koujou-san/pull/44)を全CI成功後にマージした。main `bb0c5cbb26250f2bb56b41193de91a20b32d7505`と最終head `7f98829799fb9539735074972976d5ac8be80f95`のtreeは`1ab3eff13af6798ad756922068d1e943872f9dbe`で一致し、差分0。[検証記録](2026-10-16-issue-12-evidence.md)、[独立検品](2026-10-16-issue-12-inspection.md)、マージ後照合が揃い、#12をCloseした。

## Issue #13の判定

状態: Complete。Scope revision 1。[PR #45](https://github.com/KingYoSun/koujou-san/pull/45)を全CI成功後にマージした。main `1b46ec0cef76f50f7b92faa0a43d6c6d04a0b3e2`と最終head `384ea26b1aaa3b0ff45bca3d233bba5067910087`のtreeは`87a540244da6bd53d4babf2edf1e4d8e6bcd6d86`で一致し、差分0。[検証記録](2026-10-16-issue-13-evidence.md)、[独立検品](2026-10-16-issue-13-inspection.md)、マージ後照合が揃い、#13をCloseした。

## Issue #14の判定

状態: Complete。Scope revision 1。[PR #46](https://github.com/KingYoSun/koujou-san/pull/46)を最終head `d24675c5e5e430a4e30abe5261e7082608c6b8e2`の全CI成功後にマージした。main `cdf6c14ad6a97c50db1e385a3714c84fc3102f29`と最終headのtreeは`f2fc48613f25e5c55a3312d19eac5366185e00aa`で一致し、差分0。[検証記録](2026-10-16-issue-14-evidence.md)、[独立検品](2026-10-16-issue-14-inspection.md)、マージ後照合が揃い、#14をCloseした。個別契約単価もWorkspace共有を初期値とし、履歴を無期限に保持する。

## Issue #15の判定

状態: In progress。Scope revision 1。[計画](2026-10-16-issue-15-plan.md)の三つの初期動作について推奨案の採用が確定した。版付き方針・手動固定・管理者による有効化と、自動化主体・Worker処理枠を実装し、専用PG・実Ory/MCP・Webの実操作を確認した。[検証記録](2026-10-16-issue-15-evidence.md)を参照。Windowsの前面化で技術的なエラーがあり、詳細画面の確認を継続する。独立検品・CI・マージ後照合は未完了。

#16〜#34は未着手。Runner、実行・検品の詳細画面、LLM操作などは未実装であり、段階Aや初期リリースの完成とは扱わない。
