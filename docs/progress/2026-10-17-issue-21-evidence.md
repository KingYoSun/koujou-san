# Issue #21: 固定成果物による独立検品の証跡

- 基準main: `fbb0637ff4f9de825a37a4dc808f7495517f6758`。Scope revision 1、区分C。
- 条件は[受入計画](2026-10-17-issue-21-plan.md)、方式は[ADR 0021](../adr/0021-fixed-inspection-evidence.md)、操作は[手順](../runbooks/inspections.md)を参照する。
- この記録の作成時点では独立検品・PR CI・マージ後照合が残る。完了の現在判定は[current-state](current-state.md)へ集約する。

## 実装と環境

共通Runの派生として検品定義、固定版の採用、実行意図、条件別の結果と証拠、人の判断を接続した。新規migration 017を追加し、001〜016は変更していない。共通Runの終了処理から検品の受入判定を作り、工程が使う結果にも反映する。原レポートとCLI原ログは保持する。

Windows上の二つの登録端末ID、接続、別の保存域と独立Git cloneで検証した。元の所有リポジトリには、合成の認証・実装履歴・未commitファイルを置き、固定commitの受渡し先へ入らないことを確認した。物理的に別のPCで実施したとは記録しない。OS・GUIの起動/認証/終了・S3スリープは再試験せず、有効な既存の受入を再利用した。

Codex CLI 0.162.1とClaude Code 2.1.300は、リポジトリ外の新規一時ディレクトリと既存の通常認証で実接続した。両方が同じ合成マーカーを返し終了0。Codex JSONLのイベントはthread開始、turn開始、agent_message、turn終了で、作業ツール呼出しは0。Claudeはエラーなし、許可拒否0。秘密値やローカル診断の全量は共有せず、端末のGit管理外ログに保持した。共有メモリ、資格情報とOSの全読取り、モデル側の記憶消去の完全分離は未確認であり、強制済みとは表示しない。

## 条件と証拠

| 条件 | 確認した結果 |
| --- | --- |
| AC-1 | 専用PGで明示noneと選択した固定Contextを照合。nativeの実CLIが受け取ったinputはサーバーの固定入力に一致。合成の実装履歴を自動注入せず、選択した方針だけを渡した |
| AC-2 | 二つの登録端末の割当をnativeの共通入力・worktree処理へ渡し、独立cloneの同じ固定commitで外部Node CLIを実行。未commit・認証・履歴の合成ファイル不在、元checkout/commit/status不変、別保存域を確認 |
| AC-3 | 宣言は宣言のまま、明示した機器ログは観測として記録。未知の権限能力を文字列で強制済みにできず、必須session=enforcedの接続は開始拒否。固定基準・注入の限定した保証と未確認を画面で区別 |
| AC-4 | 終了0のFAIL、形式不正、必須条件/未確認欄欠落、未実行、証拠なし・別条件/対象の証拠、非0終了をPASSにしない。任意の判定fieldの明示投影と原レポート保持を照合 |
| AC-5 | 対象digest、条件、必要なContextをそれぞれ変更・明示採用し、旧結果をStaleにした。保存だけでは採用不変。旧入力・当時の結果は不変、新Runとpreviousの関連を確認 |
| AC-6 | 元Run、変更範囲、影響、証拠の十分/不足を比較。不足はINCONCLUSIVE、固定の根拠がある差分検品は旧Runへ関連付け |
| AC-7 | 四種類の証拠由来、対立するPASS/FAILを保持。編集者の例外判断と別主体の拒否を確認。GUIから理由・未確認付きの例外判断を保存し、再取得後も元FAIL・Stale・証拠失効を保持。実行許可を付与せず |
| AC-8 | 固定証拠のavailableからexpiredへの新しい状態を表示。当時のRunと判定は不変。画面でも過去のFAIL、現在のStale、現在の証拠失効を別表示 |
| INVAR-1 | 固定定義/Manifest digest不一致、未指定条件を受入PASSにしない |
| INVAR-2 | 推奨経路はレポートのデータとして保持。判断や受入結果の保存から追加Run・権限変更を発行しない |
| INVAR-3 | 既存の全Workspace文書を読み込まず、明示した選択だけがinputへ入る。noneのnative入力にContextがないことを確認 |

## 共通画面

実Ory・専用DB・1万Context×3版を持つ所有環境で、一回の共通画面の流れを確認した。判定/現在の有効性/分離情報/条件別証拠、対立する結果の比較、人の例外判断、定義一覧→固定取得→下書き編集→明示保存→明示採用→判断を含む履歴取得が成功した。保存では採用記録が変わらず、採用は別の記録を作った。本人の画面操作は依頼していない。

表の列間隔を整え、固定参照と未確認が読める画面を保存した。`test-results/issue21-inspection-ui.jpg`に、例外判断後もFAILであること、Stale、明示的な無注入、証拠expiredを同じ画面に記録した。検証タブを閉じ、所有fixtureだけを終了した。

## ローカル検証

- `pnpm run check`、`test`、`build`成功。共通UIは12ファイル39件成功。
- `pnpm run test:inspections`成功。固定入力、負の結果、採用とStale、差分、例外判断、所在失効、現在の参照権限、メタデータ/固定範囲の履歴ページを照合。
- `pnpm run test:workflows`、`test:runs`成功。共通終了処理と既存工程・Runの回帰を照合。
- `cargo test fixed_inspection_handoff_keeps_commits_and_inputs_separate -- --ignored --nocapture`成功。実CLI二接続の受渡しは2.23秒、GUI起動なし。
- `pnpm run check:desktop`、`test:desktop`成功。nativeは11件成功、9件の明示実接続用試験を通常実行では除外し、そのうち今回の固定CLI受渡しは個別実行して成功。`test:database`も成功し、新migrationを含む保存・停止・復元を照合した。独立検品・CIの結果は完了後に追記する。

ログは`test-results/issue21-*.log`、GUIを含む実Ory試験は`.cache/issue21-ui-run.log`、実モデルの秘密値を含み得る診断は`.cache/issue21-cli-probe/*-private.json`へ保存した。後者をGitや共有レポートへコピーしない。新規のinspectionコードがCodeGraphの索引にない場合は、未登録を確認してから直接読んだ。索引を新規作成・更新していない。参照指定のDESIGN.mdとADR 0014は現行checkoutに存在せず、既存の共通画面構成に沿った。
