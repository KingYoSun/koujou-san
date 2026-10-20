# Issue #33: 実S3証拠の独立補完検品

総合判定はPASS。新しい固定証拠により、Issue AC-11、設計書AC-24、Issue AC-10のリリース受入に残っていた実S3の確認を完了した。[旧独立受入検品](2026-10-20-issue-33-acceptance-inspection.md)の有効なPASSと合わせ、固定対象の全受入条件を満たすと判定する。旧記録のINCONCLUSIVEは当時の未実施を表す記録として保持し、本記録で補完する。

## 対象と入力

- 対象commit: `b170b13b7166c9bb045b72a772bcb34634f58a3b`。検品開始時のHEADと一致し、追跡ファイルの作業差分はなかった。
- 関連: [Issue #33](https://github.com/KingYoSun/koujou-san/issues/33)、Scope revision 3、区分C。[計画](2026-10-19-issue-33-plan.md)のS7、Issue AC-11・AC-10、設計書§14.3 AC-24を今回の判定対象とした。
- 検品者・実施日: 実装会話を継承しない別コンテキストのCodex（`issue33_sleep_final_review`）、2026-10-21 JST。
- 目的: [新しい実S3証跡](2026-10-21-issue-33-sleep-evidence.md)と固定原記録を照合し、旧検品の残る3条件を判定する。
- 対象外: 実装変更、R1〜R4と既存68条件の再検証、新しい有料要求、実機操作、追加スリープ、API mutation、外部書込み、commit/push、公開リリース、#34。
- 入力: Issue本文、計画、設計書AC-24、旧検品、68条件の対応表、固定索引と原JSON・NDJSON・共通UIのAXテキスト・JPG、依頼で示された本人の復帰報告。Issue本文は`gh issue view 33 --repo KingYoSun/koujou-san --json body`で読み取った。
- 渡さない情報: 実装者の会話履歴と自己評価は自動継承せず、判定には固定証拠を使った。`.env`、`*private.json`、DPAPI、App鍵、元CLI秘密ファイルは読んでいない。
- 実行環境: Windows、PowerShell、Python 3.13、Git/GitHubの読取り。索引対象外の文書・証拠を照合したため、CodeGraphによるコード探索は行っていない。

旧検品の対象`b43f77e2f8d49e1952a56b3b6ce250509945272b`から対象commitまでの差分は、対応表・受入記録・旧検品・current-stateの4文書だけだった。R1〜R4、AC-1〜9、INVAR-1〜3、全68条件と162リンク、検索・探索の更新/再構築/部分結果/合意負荷の照合結果は、旧検品の有効なPASSを再利用した。同じテスト・有料要求・実機確認は繰り返していない。

## 証拠の固定と整合性

新索引`.cache/issue33-native/sleep-evidence-index.json`のSHA256は`363f6aa3b0e31608e006ed6441c95af0cdc2c52a34edd39926411bd80d1e33ba`で指定値と一致した。列挙された17ファイルは、すべてサイズとSHA256が一致した。

旧索引は`.cache/issue33-native/acceptance-fixed-b43/acceptance-evidence-index.json`、SHA256は`ab607004aec47fc252bba86a5b678bf65cbf8cb5b3ca9eb8513cae82dd14123a`。`snapshot-map.json`で元相対パスと凍結コピーを対応付け、旧35ファイルもサイズ・SHA256の一致を確認した。現時点の環境補助設定を旧観測の代わりには使っていない。

主要な新証拠のSHA256は次のとおり。特記のない証拠名は`.cache/issue33-native/`配下を指す。ほかの証拠のサイズ・SHA256は新索引に固定されている。

| 証拠 | SHA256 |
| --- | --- |
| `sleep-power-events.json` | `98ccdf59b33fc4f44a7101706d461a632dc747fe3ceae828421b5c6a5bd01bcd` |
| `sleep-wake-events.json` | `8c73f2616001f926711ce4e091133be51872fbcd1e095d4b6a2e6ed03cf3abb0` |
| `sleep-final.ndjson` | `76557925062b781aa366eab9011cfedc9da0a3dea43d7b175820c0fdab0e1338` |
| `sleep-workflow-final.ndjson` | `04b762f19a4fbcf9ecd2f80192446567414cc39f0453997c2388224b2d0e085e` |
| `sleep-ui-after.txt` | `2d47888999222be8b6b9c1d134040b56ede26dfdecb0d9e1a16fd11d2e7d5d53` |
| `sleep-ui-after.jpg` | `74ef903a39be9e0e3dcd0b6d741f485ccfe45de45d0f83dcfe8ace93172a52c5` |
| `docs/progress/2026-10-21-issue-33-sleep-evidence.md` | `159ea20c5a4656a0aa1a28257d203181a3237991657af142a87f4d318560ba31` |

## 原記録から確認した経路

開始要求には本人の「今からスリープ確認ができます」が保存され、開始時刻の判断への回答と受付は同じoperation IDに対応していた。前後の観測は、それぞれ`2026-10-20T14:35:56.318Z`、`14:37:55.913Z`。この間のKernel-Power 42/107とPower-Troubleshooter 1はいずれもTargetState/EffectiveStateが4だった。ローカルWindows SDK `10.0.22621.0/um/winnt.h:17076`の`PowerSystemSleeping3 = 4`とも照合した。

Power-TroubleshooterのSleepTimeは`2026-10-20T14:36:43.8214400Z`、WakeTimeは`2026-10-20T14:37:24.8944852Z`。Kernel-Powerのイベント作成時刻差8166ms、Power-Troubleshooterの時刻、原stdoutの時刻は別々の観測として扱った。イベント作成時刻差を物理的な睡眠時間とは断定していない。

| 照合対象 | 確認結果 |
| --- | --- |
| Runner | 前後ともPID 84612、親PID 50640、開始時刻`2026-10-20T23:33:46.32451+09:00`で一致 |
| CLI | 前後ともPID 77888、親PID 84612、開始時刻`2026-10-20T23:34:30.045738+09:00`で一致 |
| Runと入力 | 前後とも`run:XHAYZBOL2BVSJ55GAFXVSI24UQ`、input版`OWGHBSFG4UI57C7IGTYMFHM47L`で一致 |
| 割当と実行 | 世代`VMOMVAI6NCX5QLXGWPVQRJ2T54`、Runのstarted_at、作業領域・対象commitが一致。復帰後もrunning・observation=known |
| 起動 | 前後のlaunchesは1。原イベントのstartedも1件で、無条件の追加起動はない |
| 原出力 | stdout 313件のnumberが0〜312に完全一致し、全件PID 77888。stderr 1件の`SLEEP_STDERR_ORIGINAL`を保持 |
| 共有報告 | event sequenceが1〜318に連続し、同じ割当世代。原Run export内のrecordのID/版は重複なし |
| 未送信記録 | 前後ともpending_frames/pending_usageはfalse。終了後の観測もfalse |
| 正常終了 | 最終Run版`AHJKP352RWDKCBUI52OWG2WZBK`はsucceeded、exit_code=0、observation=known、reported_through=318 |
| 工程 | `workflow-execution:1887-sleep@LT5VCQ7UCTUHNI3FYGPBNW3XM3`はcompleted、count 1。sleep taskは同じRunの最終版を参照してdone |

Run exportは968フレーム・966 recordで、beginと末尾completeが同じRunの最終版を指し、末尾の件数も一致した。工程exportは995フレームで、beginと末尾completeが同じ工程版を指す。両方ともerrorフレームは0件。最終Run・工程の内容は`sleep-final.json`の内容と一致した。

共通Web UIのAXテキストとJPGを照合し、同じRun ID・固定版・割当世代、正常終了、観測確認済み、終了コード0、最後の受理報告318を確認した。初回未開始RunにはUnknownの表示が残り、当該S3対象Runのknownと区別されていた。

## 条件ごとの結果

| 条件 | 確認した経路・証拠 | 判定 | 未確認事項・限界 |
| --- | --- | --- | --- |
| Issue AC-11 | 本人が指定した開始時刻、実S3イベント、前後のRunner/CLIのPID・開始時刻・親PID、同じRun・入力・割当世代、原出力と未送信記録、追加起動なし、正常終了と共通画面を上記固定証拠で照合 | PASS | 非LLM CLI fixtureによるWindowsの一回の観測。全CLIの同等動作へ広げない |
| 設計書AC-24 | 通信断時のpending保持・復帰後同期・割当期限だけで再配布しない既存の有効な検品に、今回の実S3後の同じCLI・Runの継続、原出力の重複/欠番なし、knownと既存Unknownの区別を補完 | PASS | S3の観測前後のpendingはfalse。S3中に未送信記録が新規発生したとは主張しない |
| Issue AC-10のリリース受入完了判定 | 旧検品の全68条件・162リンクと両エンジンの照合結果を再利用し、唯一残っていたIssue AC-11/対応表AC-24の実S3未確認を本記録で解消 | PASS | PR #70の最新必須CI、merge後の対象照合、Issue Closeは本検品の対象外 |

## 指摘と未確認事項

| 指摘 | 関係する条件 | 到達経路と具体的な影響 | 根拠 | 分類 |
| --- | --- | --- | --- | --- |
| 旧検品時点の実S3未実施 | Issue AC-11・AC-10、設計書AC-24 | 旧時点では受入完了を保留していた。今回の実観測で残条件を満たし、保留理由を解消 | 新索引17件、上記の条件別照合 | 既定条件の未確認を解消。blockerではない |

blockerは0件。今回の固定条件の未達、合意済み挙動の回帰、新要件として扱う指摘はなかった。

原stdoutの最大間隔はnumber 142→143の30.935670137秒であり、出力欠落や物理的な睡眠時間とはみなしていない。操作案内の20〜30秒は目安で、ACの最小時間ではない。正確な物理睡眠時間や別端末・別CLIの性能は今回の判定範囲に含めない。

既存のunconfirmed_execution=trueは前後・終了後とも維持されている。S3対象Runの継続確認を、過去の全実行の所在確認へ広げない。Codexの再起動は、証跡に記録されたCLI正常終了後の操作ツール復旧として扱い、再スリープや検証CLI再実行の証拠とは扱っていない。旧検品が保持したFAIL・INCONCLUSIVE・未取得添付・料金未設定・利用量欠測も成功や0費用へ変換していない。

## 判定

- 総合判定: PASS。旧検品の有効なPASSと本記録を合わせ、Issue AC-1〜11・INVAR-1〜3、設計書の全68条件について、固定対象のリリース受入に残る未確認はない。
- 実行した検証: 新索引と17ファイル、旧凍結35ファイルのサイズ・SHA256照合、原JSON/NDJSONの構造・件数・固定参照・連続性・結果の照合、共通UIのAXテキスト/JPGの照合。製品テスト・有料要求・実機操作は再実行していない。
- 未実行: PR #70の最新必須CIとmerge後の照合。受入PASSを、merge済み・Issue Close済み・公開済みとは読み替えない。
- 修正後の再確認: 必要な修正はない。条件を判定し終えたため、本補完検品を終了する。

本記録はyomiyasuの業務報告・説明の観点で一度推敲し、固定参照、数値、条件、否定、未確認の範囲を原証拠と再照合した。lintの見直し候補は箇条書き比率37.5%の1件であり、対象・入力・判定を並列に示す雛形の項目として保持した。保存した成果物は本記録だけであり、ソース、固定証拠、旧検品記録は変更していない。
