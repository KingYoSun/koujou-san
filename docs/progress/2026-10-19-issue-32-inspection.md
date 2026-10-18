# Issue #32: 固定成果物の独立検品

## 対象と入力

- 対象commit: `8ef65c23c89a306be0e7e950485ffa072eefdac4`
- 基準commit: `cfca32923449ce6f24347088c12ea0ad1834b204`
- 関連Issue: [#32](https://github.com/KingYoSun/koujou-san/issues/32)、区分C、Scope revision 1
- 検品者・実施日: 実装履歴を継承しない別コンテキストのCodex、2026-10-19
- 入力: `AGENTS.md`、`AGENTS.local.md`、`docs/README.md`、開発手順、Issue lifecycle、Issue #32本文、[計画](2026-10-19-issue-32-plan.md)、[実装証跡](2026-10-19-issue-32-evidence.md)、[ADR 0032](../adr/0032-cost-quality-comparison.md)、固定差分、下記の生証跡。
- 目的: 本人が承認した、期間・Scope・モデル・Context/工程の固定版などで二群の条件を保存し、サーバー集計を並べる画面について、AC-1〜6とINVAR-1〜3を判定する。
- 対象外: 全件のブラウザ集計、全Contextの事前列挙、実モデル消費、自動モデル選択・必須Contextや検品の変更、新しい性能SLO、本番API・新OAuth・外部変更・Windows実機の追加操作。
- 渡していない情報: 実装セッションの会話履歴と非公開の資格情報。独立検品に不要な実装者の推論は引き継いでいない。引継ぎの自己評価だけで合否を決めず、コードと記録を照合した。
- 環境: Windows/PowerShellの共有checkout。検品開始時のHEADは対象commitと一致し、作業ツリーはclean。コード探索には既存のCodeGraphを使用した。`DESIGN.md`とADR 0014は欠落しており、追加の視覚基準は設けていない。

## 証拠

| 記号 | 確認した成果物・観測結果 |
| --- | --- |
| E1 | `apps/server/internal/usage/query.go`、`comparison.go`、`service.go`。既存usageの保存・重複排除・現在認可・十進数・固定位置cursorを共用し、比較条件・詳細・品質の照会を追加している。 |
| E2 | `apps/server/internal/usage/comparison_test.go`、`postgres_test.go`と `test-results/issue32-final-pg.log`。所有する六計測・五帰属・二Scope、実測0、欠測、非該当、未確定数量、固定入力、品質、時間、再評価、非公開単価・Context、ページ継続の実PG試験がPASS。 |
| E3 | `apps/server/internal/mcpapi/usage_test.go`、`test-results/issue32-mcp1.log`。HTTP/MCPの固定版と集計・exportの一致、単価非公開化後の読取り拒否・金額と参照の除外、現在roleの再確認がPASS。 |
| E4 | `apps/client/src/usage-comparison*.tsx`、`usage-comparison-model.ts`、`usage-record-details.tsx`、`usage-reevaluation.tsx`。二群条件、50件単位の結果、範囲と欠測の説明、根拠、手動再評価、既存編集への導線を確認した。 |
| E5 | `test-results/issue32-usage-ui-observed.json` と `issue32-ui1.log`、実装証跡の共通画面操作記録。比較条件の新版、0.001から0.002への料金更新、0.100000 USDから0.200000 USDへの明示再評価と旧評価参照、新Context/定義の比較条件が保存されている。Scope選択、Tab/Enter、根拠と三編集先のfocus、exportの観測は操作記録に基づく。 |
| E6 | `test-results/issue32-follow-up.json`。次Runはqueued・未割当・未開始で、新Context `BGY6FSGXYDQ2V33DR7YYSGEF53` と新定義 `7XGPSE3OCX664TSHQK6EFAWNCU` を入力参照に持つ。元の二Runはそれぞれ旧Context版を保持する実DBのassert。 |
| E7 | `test-results/issue32-coverage-final.log` と `issue32-final-pg.log`。1万件×3版・100 Scopesのサーバー集計から有界ページと継続cursorを返す試験がPASS。全体比較の実測は前者15.57秒、最終PGは15.49秒。Scope絞り込みの既存集計はそれぞれ約0.76秒、約0.73秒。 |
| E8 | `test-results/issue32-check-final.log`、`issue32-test-final.log`、`issue32-build5.log`。静的検査・型・format、Go全package、UI 19ファイル71試験、共通UI buildがPASS。 |
| E9 | `test-results/issue32-before.log` は変更前の比較API未実装で `invalid_usage` によりFAIL。`issue32-ui1.log` は画面操作後の旧MCPトークンでUnauthorizedとなり後続試験がFAIL。再接続後の `issue32-ui-harness2.log` は後続を含め84.49秒でPASS。画面操作の保存結果と、再接続ハーネスの結果を区別した。 |

## 条件ごとの結果

判定は固定したfixture・利用場面と、提示された実操作の観測結果に対するもの。検品者によるモデル実行や画面再操作は行っていない。

| 条件 | 確認した経路・操作と結果 | 証拠 | 判定 | 未確認事項 |
| --- | --- | --- | --- | --- |
| AC-1 | 同じ期間のRun・操作・Hopper・Triage・未帰属を扱う。二Scopeの配列一致で対象を選び、既存projectionの最新記録から一消費を一度だけ集計する。六計測の件数と二Runの時間をassertし、HTTP/MCP・元記録・集計を照合する。画面は期間、帰属、評価種別、反映位置を示す。 | E1〜5 | PASS | なし |
| AC-2 | 数量のnullと実測0、未取得、非該当、一部計測、未確定範囲、未算定を区別する。未確定数量は確定合計へ含めない。報告額・推定額・API換算額を一種類ずつ選び、通貨・契約・計測範囲を別表示する。根拠から原計測・料金固定版・評価へ進め、API換算額を支払額や節約額と呼ばない。 | E1〜5、E8 | PASS | なし |
| AC-3 | モデルとContext/工程の固定版を条件として保存・再取得し、条件と表示ページを書き出す。品質は独立検品の固定条件版・保存時判定、CLI自己申告、未確認を分ける。手戻りは明示された前Run参照、時間はRunごとに一度だけ集計する。期間・評価種別・反映位置・検品条件・取得範囲の違いを表示し、異なる通貨を合算しない。 | E1、E2、E4〜6、E8 | PASS | なし |
| AC-4 | 料金保存と再評価は既存の書込み認可・固定参照を通る。料金の新版だけでは旧評価を変えず、明示再評価で前評価を参照した新評価を作る。非公開単価の金額・参照は比較へ混入せず、非公開Contextに依存する保存条件も読めない。既存一覧・集計・MCP・exportも同じ認可を維持する。 | E1〜5 | PASS | なし |
| AC-5 | 根拠の取得後、通常のモデル接続・Context・工程定義へfocus付きで移動する。手動編集した新版は通常の検証・明示採用を経て次Runの固定入力となり、比較条件でも区別できる。未計測の新版は「記録なし」と表示する。比較の取得・保存・export・料金更新・再評価には、モデル・必須Context・検品を変更したりRunを開始したりする呼出しがない。 | E1、E4〜6。通常のRun搬送は `apps/server/internal/runs/usage.go` の固定入力依存も確認 | PASS | なし |
| AC-6 | サーバーが集計・品質・詳細をcursorで返し、画面は50件ずつ置き換えて表示する。Scope候補もcatalogのページを使い、Context本文や全利用記録を先に取得して集計する経路がない。1万件×3版の有界ページと、画面操作記録のkeyboard・focus・範囲/未確認理由・詳細取得を照合した。 | E1、E4、E5、E7、E8 | PASS | 速度目標は固定条件にないため判定していない |
| INVAR-1 | 保存済みの帰属を変更せず、Run・操作・Hopper・Triage・未帰属で別の集計行を返す。品質・時間のRun件数はモデル要求数や通貨数で増えない。 | E1、E2、E4 | PASS | なし |
| INVAR-2 | 独立検品の条件版と保存時判定を示し、現在の有効性は通常の検品詳細で確認する旨を表示する。CLI自己申告・未確認を独立検品PASSへ変換せず、計測状態・計測範囲・契約・欠測を分けている。 | E1、E2、E4、E8 | PASS | なし |
| INVAR-3 | 未取得数量・未算定費用を0へ補完せず、新版の未計測も記録なしとする。比較は条件の保存だけを追加し、設定変更・採用・Run作成は通常の明示操作として残す。 | E1、E4〜6 | PASS | なし |

## 保存・認可・共通処理の確認

`service.go` は比較のScope・固定入力・帰属先を依存参照へ保存し、現在の権限を確認する。`query.go` は各ページで現在の主体と権限を確認し、金額の左結合にも認可filterを適用する。非公開の料金評価を参照できない場合は金額と料金参照を返さず、元の閲覧可能な計測を未算定として扱う。保存済み条件やexportもこれらの結果を用いる。

比較条件の `basis` は固定版をすべて含む利用記録に限定する。実運用のRun利用量の搬送も `runs/usage.go` から `inputRefs` を依存として保存するため、fixtureだけに固定版が付く構造ではない。検品由来のRunでは既存 `inspections/results.go` の保存時assessmentを用い、比較側で新しい品質判定を作らない。

migration 25はusageの比較種別と二つの索引を追加する。過去のmigration・原計測・料金・評価を書き換える差分はない。既存の一覧・集計・exportには、空の `basis` で従来の対象を維持する。ネイティブの実行境界を変更する差分もない。

## 指摘と判定

- 総合判定: PASS。AC-1〜6、INVAR-1〜3をすべて判定した。
- blocker: なし。固定条件の未達、または今回の変更による合意済み挙動の回帰として、具体的な到達経路を示せる指摘はない。
- 必要修正: なし。新たな性能SLO・操作方式・Context選択要件は追加していない。
- 検品で実施した確認: 固定commit/基準差分、現在のソース、テスト内容、PG/MCP/UIの観測結果、実DB assertの静的照合。対象差分の `git diff --check` はPASS。
- 実行していない検証: テストの再実行、画面の再操作、本番API・モデル・新OAuth・外部書込み・Windows実機。今回は固定成果物の独立検品であり、既存の生証跡を評価した。初見操作や実S3スリープなど、#33へ移管済みの未確認事項も完了へ変更していない。
- 失敗の扱い: 変更前FAILは再現証拠。画面後のMCP失効FAILは記録を保持し、保存済み画面結果と、修正後ハーネスのPASSを別の証拠として扱う。
- 残る終了処理: 実際に発生する必須PR CI、配布の更新・復旧に必要な確認、merge後照合とIssueの手動Closeは実装担当が進める。この検品記録だけでIssue全体をCompleteとはしていない。
- 再検品: 必要修正はない。対象commit以後に実装が変わる場合は、その差分と影響する条件を照合する。

本記録のみを追加し、実装・テスト・既存文書は変更していない。commitは行っていない。
