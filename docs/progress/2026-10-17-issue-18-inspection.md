# Issue #18: 独立検品

- 対象: [計画](2026-10-17-issue-18-plan.md)のScope revision 1、AC-1〜8・INVAR-1〜3。
- 基準main: `bef3bf7402ce8f78f421616a27189e1f02acb512`。
- 一次固定commit: `d2d41f8395cc387bca73723c3118a3ec3cf1e5e5`。
- 修正固定commit: `636dcbb9b675a819d358d98c2e688c6e31aa26f2`。
- 担当: 実装履歴を継承しない独立コンテキスト `/root/issue18_independent_review`。
- 追加のWindows受入固定commit: `fa4822f88c21ca29a8cca7da3271bbdb89c4115a`。
- 結果: 全AC・INVARと追加のWindows受入を確認。blockerなし。最終head CIとマージ後照合は別に行う。

## 判定

| 条件 | 判定と根拠 |
| --- | --- |
| AC-1 | 設定→端末認証→実child→原出力・終了報告のコードと実接続証跡を確認 |
| AC-2a | 一次は無注入Manifestで未達。固定した製品関数を別crateで呼び、`input_binding_unsupported`を再現。修正後は同じ局所検証と実Runnerのpromptのみ・終了0を確認 |
| AC-2b | 未対応bindingの準備待ち、process未起動、入力保持を確認 |
| AC-3 | 現在の割当・世代・方針を再確認してローカルprogramを起動。参照・preview・準備からtaskを起動しない経路を確認 |
| AC-4 | 共通入力処理と、Codex/Claudeの同じ入力・契約での終了0・PASSを確認 |
| AC-5 | 二Pauseの受領・配送・再開の別記録、自然文の疑問文でPauseを作らないことを確認 |
| AC-6 | 実終了と取消要求を区別し、非対応CLIの自然終了と既存成果の保持を確認 |
| AC-7 | Web回答に加え、修正固定commitのBob画像・記録で別利用者の二回答と配送/再開・終了0を確認 |
| AC-8 | 実path・環境変数参照のローカル保存、共有しない診断原ログの未取得参照を確認 |
| INVAR-1〜3 | 版・実環境・確認状況の対応、原出力と派生判定の分離、CLI権限の自動制限を保証しない表示を確認 |

二次のコード差分は無注入Manifestの省略と入力テストに限定され、別crateでの再実行・既存入力テスト1件・`git diff --check`が成功した。新たなコードblockerはなかった。

## Windows受入の追加検品（09:06時点）

固定commit `fa4822f88c21ca29a8cca7da3271bbdb89c4115a` の画像3点とJSONを担当が直接照合した。native設定の保存完了、GUI→Runner→CLIのPID対応、GUI終了後の二回答・配送・再開・終了0/Succeededが一致した。最初の回答後もCLI PIDは同じで、CLI終了後の09:02:39 JSTにも同じRunnerが受付中・active 0・未送信なしであった。

Runner/APIの実装差分はなく、footerとコメントの訂正も合意済み挙動と整合した。追加blockerはなく、従来の未確認二項目を確認済みと判定した。`git diff --check`も成功した。進行中の最終head CIとマージ後照合を別に確認する。
