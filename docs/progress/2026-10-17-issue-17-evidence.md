# Issue #17: Run割当とworktreeの検証

- 基準main: `c4d1e27322ab4c66bd9679f5fc33ff1f07c8ad54`。
- 対象: [計画](2026-10-17-issue-17-plan.md)のAC-1〜7・INVAR-1〜3。
- ユーザー判断: repositoryを使うRunごとにworktreeとbranchを自動作成する。
- 契約は[ADR 0017](../adr/0017-run-assignment-and-worktrees.md)。現在の判定は[current-state](current-state.md)。

## 実行済み

専用PGの`pnpm run test:runs`はPASS（3.18秒、package 4.348秒）。二つの端末資格情報からの競合を一つの割当へ確定し、同一再送、現在版の取得、依頼者/担当端末/Runner/CLI主体、固定入力、準備と起動の分離を確認した。未確認能力・不足・入力方式不一致・端末不在は起動しない。現在の利用方針、実行許可、端末許可、入力依存の取消と、古い割当世代を拒否する。固定Manifestの本文と未取得成果物も担当端末への取得で照合した。

起動報告後の期限切れではRunningとUnknownを保ち、同じ世代のrenewでもCLI観測をKnownへ補完しない。取消だけでFinishedを作らず、別端末への再配布も行わない。旧Run/Pause/出力/exportの試験も通過した。

Windowsの`test:desktop`は通常6試験PASS。新しいGit試験は、所有する一時repositoryから二Runへ別worktreeとbranchを作成し、一方の編集が他方や元checkoutへ反映されないこと、同じRunの再試行が編集内容を消さないことを確認した。通常のroot check/testは成功し、UIは11ファイル36試験PASS。

初回のWindows試験はworktree作成で失敗した。Git for Windowsが`//?/C:/...`形式の作成先を拒否することを同じ一時repositoryで再現し、canonicalize後のverbatim prefixをGit用の通常形式へ戻して成功した。テストを緩和したり、元checkoutへ切り替えたりしていない。

## 残る確認

最終実装のdesktop build、実OryとWeb/Windowsの画面操作、実際の二worktreeの固定commit/branchと保存記録の照合、固定成果物の独立検品、最終head CI・マージ後照合はこれから確認する。実CLI起動・対話・OSプロセス復旧をこのIssueの完了へ含めない。
