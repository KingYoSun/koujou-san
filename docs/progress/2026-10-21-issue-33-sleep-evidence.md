# Issue #33: 実S3スリープと復帰の記録

対象は[一巡の計画](2026-10-19-issue-33-plan.md)のS7、Issue AC-11、設計書AC-24とAC-10の残る観測である。本人が2026-10-20に実施時刻を指定し、Windowsのスリープと復帰を一回行った。以前の通信断試験や、単なる画面ロックで代替していない。スリープの再実施は不要である。

## 固定した環境と対象

実装は `b170b13b7166c9bb045b72a772bcb34634f58a3b`。Windowsのrelease Runner、実Ory・PostgreSQL・Bleve・ArcadeDBと共通Web UIを使った。残るOS境界の確認には既存の非LLM CLI fixtureを使い、新しいCodex/Claude Code・API要求は送っていない。検証fixtureの能力を、全ての外部CLIの同等動作へ広げない。

- 工程: `workflow-execution:1887-sleep`。本人の「今からスリープ確認ができます」を受け、通常APIの開始時刻の判断へ回答した。
- Run: `run:XHAYZBOL2BVSJ55GAFXVSI24UQ`。
- 固定input: `run-input:XHAYZBOL2BVSJ55GAFXVSI24UQ@OWGHBSFG4UI57C7IGTYMFHM47L`。
- 対象commit: `82ff9f50d37b89ad8c7b58b9ba1baadb2a719b8d`。
- 自動作成したbranch: `koujou/run-77377b7d51c6a45806afdfb86fe669de`。通常のkukuri checkoutは変更していない。
- 割当世代: `VMOMVAI6NCX5QLXGWPVQRJ2T54`。

## 実スリープと同じプロセスの継続

Windowsの `powercfg /a` はS3を利用可能、S0は非対応と示した。実施後のKernel-Powerイベント42・107はTargetState/EffectiveStateとも4。インストール済みWindows SDKの `winnt.h` にある `PowerSystemSleeping3 = 4` と照合した。Power-Troubleshooterイベント1も同じ状態を記録した。

Power-TroubleshooterのSleepTimeは `2026-10-20T14:36:43.8214400Z`、WakeTimeは `2026-10-20T14:37:24.8944852Z`。JSTでは同日23:36〜23:37の一回である。Kernel-Powerのイベント作成時刻の差を、実際の睡眠時間とは断定しない。元イベントの時刻と、CLI出力の時刻を分けて保存した。

| 観測 | スリープ前後の結果 |
| --- | --- |
| Runner | PID 84612、開始時刻 `2026-10-20T23:33:46.324510+09:00` が一致 |
| CLI | PID 77888、開始時刻 `2026-10-20T23:34:30.045738+09:00` が一致。親PIDも84612で一致 |
| 起動数 | `launches.jsonl` は一件。再開用の追加プロセスを作っていない |
| 共有状態 | 同じRun・input・割当世代のままrunningを再観測。CLI出力も継続 |
| 原出力 | stdoutの0〜312、合計313件が連続。同じPIDで、欠番なし |
| 時刻の間隔 | heartbeat 142→143の最大間隔は約30.936秒。出力の欠落とは扱わない |
| stderr | `SLEEP_STDERR_ORIGINAL` を保持 |
| 未送信記録 | 観測前後ともpending_frames / pending_usageはfalse。終了後も未送信は残らない |

初回の未開始Runに関する既存のunconfirmed_executionは残した。今回のS3確認を、過去の全実行の所在確認へ広げていない。

## 正常終了と共有画面

同じCLIの継続を確認した後、当該Runの作業領域にだけ終了用のfixtureファイルを置いた。同じRunが終了コード0でsucceededとなり、工程もcount 1のcompletedとなった。停止要求だけで停止済みと判断していない。

固定Run版は `AHJKP352RWDKCBUI52OWG2WZBK`。通常の共通Web UIで再取得し、このRun ID・固定版・割当世代、正常終了・観測確認済み・終了コード0・最後の受理報告318を照合した。工程の完了も一覧へ反映された。閲覧者は既存の編集者・実行依頼不許可のままで、権限を追加していない。

スリープ後の画面照合では、Codexの操作ツールがkernel assetsの書込みエラーで起動できなかった。本人の選択でCodexだけを再起動し、復旧したChrome操作から同じ固定版を確認した。この再起動は検証CLIの正常終了後であり、スリープを再実施したり、CLIを再実行したりしていない。

## 保存した証拠と判定

原記録は `.cache/issue33-native/` に保存した。`sleep-before*.json`、`sleep-after*.json`、`sleep-power-events.json`、`sleep-wake-events.json`、`sleep-continuity.json`、`sleep-output-proof.json`、`sleep-final.json` と固定NDJSON、`sleep-ui-after.txt/jpg` が対応する。Run・工程exportは固定版と末尾completeを確認し、SHA-256を保存した。

実接続の消費はCLI 5/6回・OpenAI API 18/20要求のまま。既存の[独立受入検品](2026-10-20-issue-33-acceptance-inspection.md)を再利用し、今回追加した観測に関わるAC-11・AC-24・AC-10だけを別コンテキストで再確認する。全条件の最終判定、CI・merge後の照合が揃うまでIssue #33はCloseしない。#34は保留を維持する。
