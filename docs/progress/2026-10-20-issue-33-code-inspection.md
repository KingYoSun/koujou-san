# Issue #33: PR #70の独立コード検品

総合判定はPASS。R1〜R4にblockerはない。これはPR #70の実装差分に対する判定であり、Issue #33全体の受入PASSではない。現在の到達点と残作業は[current-state](current-state.md)を参照する。

## 対象と入力

- 対象commit: `2157861c778e8daabad302ae78500548582f42a4`。
- 比較基準: `35995f473e58ee0d27399acaa60d1ebf9ae3cd79`。
- 関連: [PR #70](https://github.com/KingYoSun/koujou-san/pull/70)、[Issue #33](https://github.com/KingYoSun/koujou-san/issues/33)、[一巡の計画](2026-10-19-issue-33-plan.md)。
- 検品者・実施日: 実装者とは別コンテキストのCodex（`issue33_code_review`）、2026-10-20 JST。
- 手順: [Issueから実装する手順](../runbooks/issue-lifecycle.md)の区分C。実装者の会話履歴や自己評価を自動継承せず、固定commit、R1〜R4、関連仕様、指定された観測ログを入力にした。
- 実行環境: Windows、PowerShell、リポジトリの既存Rust・Node.js・Go環境。`.codegraph/`があるため、コードの理解・探索にはCodeGraphを先に使った。

`git diff --name-status`で対象の12ファイルを確定し、実装・試験・開発コマンドの変更と関連する実行・認可・保存・送信経路を確認した。対象コードは`runner.rs`、`client-selection-host.ts`、`mcpapi/ui_test.go`、`mcpapi/workflows.go`、`publications/live_test.go`、`scripts/dev.mjs`と`package.json`。残る5ファイルは進捗文書である。確認中のHEADは対象commitのままで、追跡ファイルに作業中の差分はなかった。

未追跡の`scripts/acceptance*.mjs`は、今回限りの試験補助として製品コードのレビュー対象から除いた。認証ファイル、`.env`、App秘密鍵、`.cache`の秘密設定は読まず、ファイル編集、commit、push、外部書込み、新しい実GitHub接続、有料モデル要求、実機操作は行っていない。本記録の保存だけを後続の依頼で行った。

## 条件ごとの結果

| 条件 | 確認した経路・期待結果 | 証拠 | 判定・検証上の限界 |
| --- | --- | --- | --- |
| R1 | Windows Runnerが起動するGitだけに長いパスの設定を渡す。元repoの保存設定・編集・branchと、Runごとの分離・再準備を保持する。他OSのGit呼出しを変えない | `apps/client/src-tauri/src/runner.rs:30`のGit共通処理と`prepare_area`、既存worktree試験への追加、修正前後ログ、Windowsでの局所再実行 | PASS。`-c core.longpaths=true`は`cfg(windows)`内の起動引数。260文字超の追跡ファイルを取得し、保存設定false、編集・branch・commit、二つのworktreeの分離と再準備を同じ試験で確認。他OSは条件付きコンパイルと差分の確認で、実行はしていない |
| R2 | MCPの既存の端末ID・テンプレート取得、生成Context.referencesの説明を、実際の認可・参照型・由来の保存契約と一致させる。一般取得、権限、wire形式、秘密情報の経路を維持する | `apps/server/internal/mcpapi/workflows.go:65`以降、`clientops/definitions.go`、`workflows/capabilities.go`、`clientops/generation_apply.go`、`apps/client/src/client-selection-host.ts`の`initialMessages` | PASS。変更は説明文のみ。定義生成時のsource限定と一般取得の分岐を保持。裸のdevice ID、固定Context参照、サーバーが付けるRun・出力の由来とdigestの説明は既存実装に一致する。新しい実モデル要求は送っていない |
| R3 | 実起票応答喪失のopt-in試験を、承認済みの`KingYoSun/koujou-san`一件に閉じる。送信前receiptで追加POSTを止め、Unknownから同じIssueを照合してその一件だけをCloseする。通常試験・CIでは新しい実接続を行わない | `apps/server/internal/publications/live_test.go`、`publications/remote.go`、`publications/process.go`、`scripts/dev.mjs`とCI、`issue33-publication-loss-live.log`、opt-in無効時の局所再実行 | PASS。明示フラグとrepo名・repository IDを確認し、作成POST前の排他的receipt作成で追加送信を阻止する。Unknownの再処理は照合へ進み、作成応答から特定した一件をCleanupでCloseする。指定ログは`creates=1`、Issue #69への照合、試験PASSを示す。実接続は再実行していない |
| R4 | 明示的な受入確認枠だけ4時間とし、通常UIは30分を維持する。R1〜R4に不要な併存や汎用化を残さない | `apps/server/internal/mcpapi/ui_test.go:315`以降、`scripts/dev.mjs`、`package.json`、対象差分全体 | PASS。`test:acceptance`の明示フラグ時だけ4時間、通常は30分。Go試験の外枠はそれぞれ245分・35分。今回の条件に不要な新旧経路の併存や汎用化は見当たらない。時間切れまで待つ試験は行っていない |

## 実行した検証と観測ログ

| 検証 | 結果 |
| --- | --- |
| `cargo test --offline --locked --manifest-path apps/client/src-tauri/Cargo.toml runner::tests::run_worktrees_and_branches_are_distinct_and_replay_preserves_work -- --exact` | 終了コード0。対象1件PASS。ほかの実行ファイルは該当試験0件。linkerのライブラリ生成メッセージがwarningとして出たが、試験失敗はない |
| `pnpm --filter @koujou-san/client exec vitest run src/client-selection-host.test.ts` | 終了コード0、1ファイル・10件PASS |
| `KOUJOU_TEST_PUBLICATION_LIVE=0`を明示し、`.tools/go/bin/go.exe -C apps/server test ./internal/publications -run '^TestPublicationsLive$' -count=1 -v` | 終了コード0。明示opt-inがないため、設定読込み・接続前にSKIP |
| `node --check scripts/dev.mjs` | 終了コード0 |
| `git diff --check 35995f473e58ee0d27399acaa60d1ebf9ae3cd79 2157861c778e8daabad302ae78500548582f42a4` | 終了コード0 |

指定された`test-results/issue33-long-path-before.log`、`issue33-long-path-after.log`、`issue33-selection-host.log`、`issue33-check1.log`、`issue33-publication-loss-live.log`の現物を読んだ。長いパスの同じ試験は修正前に`git_preparation_failed`でFAIL、修正後にPASS。Hostは10件PASS、checkログには型検査と整形検査の成功がある。応答喪失ログはUnknownから照合への遷移、作成一回、試験PASSを記録している。実装者の説明だけを成功の根拠にはしていない。

blockerは0件。固定条件の未達または今回の回帰として、到達経路と具体的影響を示す指摘はなかった。未依頼のedge caseや将来の強化は受入条件へ追加していない。

## 検品後に共有された工程の結果

以下はR1〜R4の固定commit検品とは別の補足事実である。工程担当から共有された結果と指定されたローカル原記録を照合した。外部サービスへの再照会は行っていない。

[kukuri PR #1890](https://github.com/kukuri-app/kukuri/pull/1890)は全CI成功後、`workflow-execution:1887-final-merge`からheadを条件にmergeした。候補は`82ff9f50d37b89ad8c7b58b9ba1baadb2a719b8d`、merge commitは`92299fb938106e6a0c506128d999b176f7e336c3`。treeは`6e0821b96509932b19cbbc68cbdff9dde76e9441`で候補と一致したとの結果が共有されている。

原記録はGit管理外の`.cache/issue33-native/positive-tasks.json`、`positive-completed.json`、`kukuri-merged.json`、`exports/manifest.json`。前二つではinspection・ci・linux-package・windows-packageがPASS、mergeがsucceededで、工程は5件・completed。`kukuri-merged.json`では候補head、merge commit、tree、MERGEDを確認した。exportのmanifestには5件と各固定版・hash・completeフレームがあり、実スリープ工程はwaitingである。export後の全体停止・継続は、このmanifestだけでは確認できない。

## 対象外と未実施

人の初見操作、Windowsの実S3スリープ、全体を停止した後の継続は未実施であり、Issue #33全体の受入は未完了である。既存の通信断試験やクライアント不在時の連鎖、exportのcomplete記録を、これらの成功へ読み替えない。

今回のPASSはkoujou-sanの固定commitにおけるR1〜R4だけに適用する。kukuriの全条件、PR #70のCIやmerge後照合、初期リリース全体の完了を判定したものではない。今後そのcommitから変更する場合は、差分と影響する条件を改めて確認する。
