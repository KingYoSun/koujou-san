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

desktop build、実OryとWeb/Windowsの画面操作、実際の二worktreeの固定commit/branchと保存記録の照合を完了した。独立検品の追送照合も全末端PASS・blocker 0。残りは証跡込み最終head CI・マージ後照合。実CLI起動・対話・OSプロセス復旧をこのIssueの完了へ含めない。

## Windows実アプリで検出した差

固定60180c0の実アプリでは、割当とrenew後のGit準備が失敗し、preparedを受理しないことを確認した。所有する実pathでの再現試験はGitの `$GIT_DIR too big` を返した。Windowsのapp-data junctionをcanonicalizeすると、Codex packageの長い実pathへ展開されることを実引数で確認した。Gitの[setup.c](https://github.com/git/git/blob/v2.50.1/setup.c)には明示GIT_DIRの長さの検査があり、core.longpathsの変更だけでは解消しなかった。

作成先は設定された短いapp-data pathを保つよう修正した。失敗でbranchだけが残る再試行も、固定commitが一致する場合はそのbranchを使ってworktreeを作る。長いapp-data配置を模した試験へ拡張し、元checkout・別Run・編集中のファイルを保持する条件を維持する。診断用の出力と一時試験は製品へ残していない。修正後の実画面と実Git照合を続ける。

## 修正後のWeb・Windows実操作

最終の製品sourceは`5054f09e68e4073517ab1d9d78d2c229e05f4296`。共通UI assetは`index-Blvbpr_N.js`。実Ory・HTTP/MCP・1万Context×3版と実操作を含む隔離fixture `9bdec3738160` はPASS（1221.33秒、package 1223.262秒）。server sourceは最初の固定60180c0から変わらず、Nativeだけを修正して同じfixtureで再試行した。

| 対象 | 観測 |
| --- | --- |
| Windowsの割当 | `ui-assign-native`。依頼者、端末、Runner、Git版/主体、世代、固定inputとbranchが画面とHTTPで一致 |
| 失敗後の再試行 | 同じRun・同じ世代でrenew2→prepared3。固定版`3MZMGDOFFRVIHLT32UNAGUTJWO`、queued・started_atなし。残ったbranchから実worktree作成に成功 |
| 編集後の再準備 | 同じ領域でrenew4。固定版`ANKRGT7S33AQEWNRIZYXFWMNLX`、prepared_at不変、編集したsource.txtのSHA-256不変 |
| 二つ目のRun | `ui-assign-native-two`をAPIで割当、Windows画面から準備。固定版`W5GXSJX73AJODPIMIDQFW5YEUV`、別領域・別branch、同じ固定commit、queued・started_atなし |
| Webの共有表示 | Windowsの一つ目を`ANKRGT7S33AQEWNRIZYXFWMNLX`で取得し、担当・準備・起動未確認・固定commitが一致 |
| Webの割当依頼 | `ui-assign-web`を許可した検証端末へGUIで依頼。固定版`3Q6SY7JUTIKE6BUDQJ2NHQLKML`、担当と世代がHTTP一致、準備/起動は未確認 |

二つのworktreeで実際のGit HEADとbranchを読取り、固定commit `800cb4d51d5cb196abe3f518d25b1605dba538e5` と対応する割当に一致した。一つ目を編集して再準備しても、その編集が消えず、二つ目と元checkoutのsource.txtは元のままだった。元のbranch `master` とHEADも不変。成果の自動削除や元checkoutへの移動は行っていない。

証跡は`assets/issue17-native-{prepared,second}.jpg`、`issue17-web-{shared,assigned}.png`、対応するJSON、`issue17-git-worktrees.json`。一つ目のprepared直後と再準備後は別の固定版として記録した。共有Runの応答にローカルpath・端末資格情報の欄が含まれないことも照合した。

Nativeの接続設定は、合成した専用configを所有namespaceへCLIで準備した。画面の設定欄、版等の入力、保存済み接続の更新を確認したが、「接続設定を保存・更新」ボタン自体の実操作を成功扱いには含めない。明示したignored probeで実Oryの端末登録とDPAPI保存を通し、その端末で割当からworktree準備まで実行した。

修正版のdesktop testは6成功・6環境依存ignored、desktop buildは成功。実装headのCI run `38941724605` はLinux 4分11秒・Windows 5分3秒・GitGuardian成功。最終の証跡・検品記録を含むheadのCIとマージ後照合は別に確認する。

独立検品は実GitのHEAD/branch/hash・元checkoutのclean状態も直接読取りで確認し、全末端をPASSとした。割当前後のTriage採用値P2→P1でも全RunのID/版/payload/開始/取消が変わらないことを、独立Go overlayで直接比較した。fixture終了後、専用サービスと所有するOAuth/端末のDPAPI資格情報を片付けた。合成Git領域は証跡確認用として残している。
