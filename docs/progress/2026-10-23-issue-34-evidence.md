# Issue #34: gestaloka #94への適用記録

対象と有限条件は[計画](2026-10-23-issue-34-plan.md)、現在の判定は[current-state](current-state.md)を参照する。[固定した参照・判定・検証結果](assets/issue34-validation.json)を保存し、原本はGit管理外の`.cache/issue34/`へ保持する。実行結果を直接DBへ書き込まず、通常API、共通の工程制御、Windows Runnerを使った。

## 定義の交換と実行

koujou-sanの基準は`feae5ebd4b903ff666cad4134e09199e1a02972e`。gestalokaの基準は`f60b8ec08187feacfb55226a214cdb64a7192161`。元のgestaloka checkoutと既存worktreeは保持し、Runnerが`koujou/run-*`の専用worktreeを作った。文書更新はCodex 0.162.1、独立検品はClaude Code 2.1.300を、既存のCLI認証と新しいセッションで実行した。

| 項目 | kukuriでの#33 | gestalokaでの#34 |
| --- | --- | --- |
| 製品方針と対象 | 公開スレッドの表示・操作、取得窓の維持 | 共有世界、観測・解釈の分離、確定履歴を保持する文書の現状合わせ |
| 変更の区分 | UIの挙動変更 | 文書だけの変更 |
| 今回の条件 | K-1〜3 | G-1〜3、21ファイルの一度の照合 |
| PRの単位 | kukuri #1887の表示変更 | 複数条件を1件の文書PRへ集約 |
| 検品 | 挙動の比較試験と実表示 | 固定source、後継ADR、リンク、例示コマンドの照合 |
| CLI接続の論理名 | `acceptance-codex`・`acceptance-claude` | `docs-codex`・`docs-claude` |
| 工程 | 固定検品・三CIから条件付きmerge | 文書更新から独立検品後の完了判断 |
| Workspace | `runner-ui` | `gestaloka-94` |

接続のprogram・引数・protocol・利用者・repository対応、固定Context、Run定義、工程定義、検品基準を設定で交換した。Runner・サーバー・共有の工程制御へgestaloka専用分岐は追加していない。新しいAPIキーやモデルAPI接続は作っていない。CLIの定額契約を追加のAPI費用0円へ換算しない。

固定Contextは`sentence:gestaloka-policy@K6GVAND64JEHEYOSZVDSBHK3L7`。Issue Scopeへの所属は別レコードとして保存した。実装の固定入力は`run-input:NBXLEJ65JIHII2O462TDS3ISKP@KBWRUPGPAA4XPLYRPVGCMLG2PA`で、注入本文と固定sourceを再取得できる。本文はgestalokaの文書方針だけで、kukuriの条件を含まない。全Contextの列挙は使っていない。

工程定義は`workflow-definition:gestaloka-94@AXJT27GMYCHJWFN7CJ4ETRGQ7U`。保存・検証・採用後に明示的に開始し、`run:NBXLEJ65JIHII2O462TDS3ISKP`が一回だけ起動した。モデル実行前の準備では、古い検証URLと未登録接続、誤ったselection値・join指定を検証で検出した。端末設定と定義を直してから開始し、共通実装は変えていない。準備失敗を成功やCLI実行回数に含めていない。

## gestalokaの成果物

文書更新Runは終了コード0、派生判定PASSで終了した。これは実装者の自己評価であり、独立検品とは区別する。21ファイルの照合表を`issue94-audit.json`に保持し、13ファイルを変更、8ファイルは変更不要とした。ローカル参照119件、変更文書のPrettier、yomiyasu、`git diff --check`を確認した。実装、公開契約、設定、過去の評価結果は変更していない。

成果物は`ed39c3ce0e9e1526ad905e98b3e344a6bc9d0d99`、[gestaloka PR #96](https://github.com/KingYoSun/gestaloka/pull/96)。Ory認証、別アプリの管理画面、公開配置、世界予算とcall上限などを現行コード・後継ADRへ合わせた。[Verify v3の実行39844023536](https://github.com/KingYoSun/gestaloka/actions/runs/39844023536)は同じheadで成功した。

独立検品は`inspection:gestaloka-94@6OBUJVDF54AEVES6UHDH2UNQXD`で固定commitとG-1〜3を渡した。実装者の会話・照合表・自己評価は注入せず、空のContextと別worktreeで実行した。`run:gestaloka-94-inspection@WULE2WQ3J6RD2ZRCU2TZ74XUXH`はG-1〜3すべてPASS、未確認なし。原報告と製品の採用判定もPASSだった。

通常の工程判断へ`approved`を記録し、工程は`workflow-execution:gestaloka-94@SUIEN7V25DQANEQZYLZK6CR6JP`、count 1、completedとなった。独立検品とCIが揃ってからPR #96をマージした。merge `76f4340ecea21565c77c7906ab702e6cc6a7156c`と候補commitはtree一致・差分0を確認し、gestaloka #94をCloseした。

## 分離と費用

gestalokaに参加していない既存の別メンバーからのContext取得、実行権限照会、利用量照会はすべて403。gestaloka専用端末からkukuri側の割当取得も403だった。gestalokaのWorkspaceでkukuriのContext IDと工程IDを読んでも取得できず、各Workspaceの所有者はそれぞれのContextを取得できた。`isolation.json`と`device-isolation.json`に要求先と結果を保持する。

両CLI終了後も、kukuriの利用量集計21群は開始前と完全一致した。gestalokaの2群は今回の実装Runと検品Runだけへ帰属した。実測モデルはgpt-6.1-solとclaude-fable-5-1。CLIの2回だけを実行し、#33のCLI/API予算台帳は更新していない。費用の欠測・単価未設定を0として扱わず、費用優劣や節約率は主張しない。

## export・停止・再接続

最初の工程・実装Run・進行中検品Runのexportに続き、完了した工程と両Runの最終固定版を取得した。三ファイルとも末尾completeとerrorなしを確認し、SHA-256、版、状態、件数、サイズを`exports/manifest.json`へ保存した。既知のローカル資格情報との完全一致は0件だった。参照渡しの外部成果物と未取得の状態は維持し、本文の取得成功へ補完していない。

Runnerのactive 0・未送信frame/usageなしを確認し、通常の受付停止で終了した。専用APIと全Workerも停止し、APIのportが閉じ、Runnerが存在しないことを確認した。保存用DB・Ory・ArcadeDBと試験用認証中継は維持した。

停止中に元のCodex/Claude Codeの版取得、Gitのfetch・固定commit/tree・差分検査、元のPrettierによる13文書の検査、`gh pr view`でのmerge照会、3件のexportの構文・版・SHA-256・完了印を照合した。終了後もAPI portは閉じていた。元checkoutは基準commit・変更なしのまま。結果は`stopped-continuation.json`へ保存した。

同じAPI binaryとRunnerを再起動し、同じ利用者として認証できることを確認した。工程と両Runの最終版・状態は三件ともexportの固定版と一致し、工程はcompleted・count 1のまま。Runnerはactive 0・未送信なしで再接続し、CLIの追加起動はなかった。`restored.json`と2回のCLI実行記録に保持する。

途中の出力照会は応答を得られないことがあり、exportの取得にも時間を要した。exportは最終的に完了したが、遅延原因は特定していない。SQL計画の局所診断だけで性能改善や性能上限を主張せず、製品コード・DB設定は変更していない。

## 検証した適用範囲

今回はWindowsの既存CLI二種、同じGitHub上の別リポジトリ、文書だけの変更、固定入力の独立検品、プロジェクト別Workspaceを対象にした。共通基盤の修正は不要だった。独自CLI全般、他OS、全CIサービス、一般的な性能保証は検証していない。#33の全機能・人の初見操作・実S3の有効な証拠は再利用し、#34のために同じ確認を繰り返していない。

| #34条件 | 対応する証拠 |
| --- | --- |
| AC-1 | 計画のG-1〜3・対象21ファイル、権限、両基準commit、固定入力・Scope |
| AC-2 | 設定差分、共通Runnerの実装Run・独立検品Run・工程完了と判断 |
| AC-3 | 異なる方針と1件の文書PR、Context・メンバー・端末の拒否、費用21群の不変と新2群の帰属 |
| AC-4 | 最終3export、停止中の元ツール操作、再接続後の同じ三固定版と追加起動なし |
| AC-5 | 適用範囲と設定交換の一覧、共通修正なし、取得遅延と未検証範囲の明記 |

INVAR-1は元checkout・CI・CLI配置の保持、INVAR-2は空Contextの別検品と固定条件、INVAR-3は本人の対象指定と事前の権限記録で照合した。#34全体の独立検品、koujou-san側PRのCI・merge後照合は、別途完了を確認する。
