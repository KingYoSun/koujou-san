# 共通UIの全画面見直し（koujou-uiスキルによる監査と修正）

直接の依頼から始める。[Issueから実装する手順](../runbooks/issue-lifecycle.md)と[デザインスキル](../../.claude/skills/koujou-ui/SKILL.md)に従う。

## 依頼と範囲

- 依頼: koujou-uiスキルを用いて、共通UI（apps/client、Web／Tauri共通）の全画面を見直し、修正する。ユーザーの判断が必要になるまでは自律的に進める。
- 対象: `apps/client/src` の全画面と共通部品、`styles.css`、`index.html`。
- 対象外: API・保存データ・wire契約の変更。画面の追加・機能の追加。利用者体験が変わる構造変更は本人の判断を得てから適用した（下の「判断を得て適用した項目」）。
- 基準commit: `81e5bec`（main）。作業開始時の変更なし。
- 確認の区分と理由: B。表示・文言・部品の変更で、保存・認証・実行の経路は変えない。

## 受入条件

| ID      | 対象と期待結果                                                                                                                                         | 確認方法                                                | 結果 |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------- | ---- |
| AC-1    | 全20画面と共通部品をSKILL.mdの確認項目（配置・文言・数値・チャート・状態・やらないこと）で監査し、指摘を適用／判断待ちに分類する                       | 監査結果（287件）と本記録の分類                         | PASS |
| AC-2    | 説明段落・入力hint・値の後ろの補足文・画面名を繰り返す見出し・括弧の注記を外し、状態・結果・エラー・権限不足・空表示の短い文だけ残す                   | 静的描画テスト、1440pxの実表示                          | PASS |
| AC-3    | 入力・ボタン・札・通知・折りたたみ・一覧の件数行を共通部品と共通CSSへ寄せ、画面間で同じ意味の表示が同じ部品・同じ文言になる                            | `styles.css` の差分、1440px／1024pxの実表示             | PASS |
| AC-4    | 文字は4.5:1以上、操作境界は3:1以上。無効・注意・成功を色だけで示さず、前景と背景を対で指定する                                                         | `styles.css` の値（下表）                               | PASS |
| AC-5    | 1440pxで三ペイン、1024pxで画面選択のselectと縦積みになり、ページ全体の横スクロールがない                                                               | 全画面で `scrollWidth === clientWidth` を観測           | PASS |
| AC-6    | 判断を得た6項目（三ペイン配置換え・取得タイミング・重複操作の削除・文言とブランド・利用量の表・ページ件数）を適用し、既存の操作・API・版照合を変えない | 差分の照合、静的描画テスト、1440px／1024pxの実表示      | PASS |
| INVAR-1 | API呼び出し・要求ID・版照合・権限失効時の破棄、保存・LLM依頼・Run開始・外部起票の操作は変えない                                                        | 差分の照合、`pnpm --filter @koujou-san/client run test` | PASS |
| INVAR-2 | 新しいCSS変数・テーマ・フォントCDN・UIフレームワークを増やさない                                                                                       | `styles.css` の `:root` 差分なし                        | PASS |

## 進め方

1. 14の画面群ごとに監査担当を並列に置き、SKILL.md・DESIGN.md・docs/design/\*.md を読ませて指摘（file:line・引用・規則・修正案・利用者体験への影響）を集めた。287件。
2. 指摘を本人が判定し、DESIGN.mdが明示的に求める削除・統一（説明文、hint、重複見出し、部品・文言の統一、aria、CSS）は適用、表示位置・操作・取得タイミング・互換に関わるものは判断待ちにした。
3. `styles.css` を要素単位の共通規則へ統合し（label／input／select／textarea／fieldset／pre／summary／h2／h3／button／a）、容器ごとの重複規則と未参照規則を削除した。1485行→1224行。
4. 10の画面群で修正を並列に適用し（担当ファイルを分離）、型検査・テスト・整形・実表示で確認した。
5. 利用者体験が変わる6項目を本人へ選択肢付きで示し、全項目の採用を得て適用した（下の「判断を得て適用した項目」）。

## 適用した内容

- 文言: 操作説明の段落、ラベル・summary・ボタンの括弧注記（「（実行しない）」「（詳細操作）」「（複数はカンマ区切り）」など）、通知の後ろに続く製品前提（「自動送信しません」「…とは別の操作です」）、placeholderのhintを削除。失敗通知は「〈対象〉を〈動作〉できませんでした。」の形に統一。ページ送りは「〈対象〉の次のページ」、項目名と値の区切りは全角「：」。
- 見出し: shellのh2と重複するh3（Hopper／Issue優先度／共通の処理運用／Triage／外部起票／検品／GitHub観測／利用量／LLM接続／API接続／アカウント）を削除。全画面で h2 22px／h3 17px。
- 部品: 手書きのtabpanelをTabPanelへ、閉じたdetails内のフォーム・JSONをDisclosureへ、素のlabel+input/textareaをField（JSONは `jsonError` で `aria-invalid`）へ、状態の平文をStatusBadgeへ置換。合流札（`.workflow-join`）もStatusBadgeに統一。
- 状態辞書の統一: 質問の状態（`pauseStateNames`）、Runの状態（`runStateNames`）、クライアント操作の状態（`operationStateLabels`）、試行の結果（`taskResult`）、評価種別（`valuationOptions`）、日時（`at`）をそれぞれ一か所にし、画面ごとの別文言を削除。
- 取得中・一覧: 取得ボタンに `aria-busy` と「…を取得中…」、取得中に既取得の一覧を消さない、一覧に「N件を表示（続きあり）」と0件の空表示文、中央一覧の選択行に `aria-current`。
- 補助操作（戻る・ページ送り・再取得・外す・閉じる・履歴）は `data-variant="secondary"`、主操作（保存・依頼・採用・作成・有効化）は既定の深緑。
- CSS: label はgrid、checkbox付きlabelは横並び、入力の境界は `--input`、無効は不透明度でなく色（muted-foreground／muted）、注意札は `--warning`／`--warning-background`、成功札は `--success`／wash、タブと補助ボタンの境界は `--input`、面の角丸0、差し戻し矢印は失敗色でなくmuted、リンクは深緑の下線。`.team` の規則がシェル全体へ漏れていた問題、`.shell` の二重定義、未参照規則（`.eyebrow` `.introduction` `.context-tabs` ほか）を解消。`theme-color` を地色 `#f6f5ef` に一致させた。

## 色の確認（WCAG相対輝度、計算値）

| 組合せ                              | 比   | 用途               |
| ----------------------------------- | ---- | ------------------ |
| `#596862` / `#e9eee0`（無効ボタン） | 5.0  | 無効の文字         |
| `#81411f` / `#f5e5d9`（注意札）     | 6.3  | 注意               |
| `#285348` / `#e9eee0`（成功札）     | 7.3  | 成功               |
| `#77877e` / `#f6f5ef`（操作境界）   | 3.5  | 入力・補助ボタン枠 |
| `#183b34` / `#deedaa`（選択）       | 9.6  | 選択行の文字       |
| `#183b34` / `#f6f5ef`（リンク）     | 11.2 | 本文のリンク       |

## 実表示

`pnpm run test:chat:ui`（`KOUJOU_TEST_TRIAGE_UI=1 KOUJOU_TEST_HOPPER_UI=1` を付けて起動）の固定環境で、全20画面を1440pxと1024pxで開いた。いずれも `document.documentElement.scrollWidth === clientWidth`。作業詳細（成果・検品／質問・判断タブ）、Hopperの評価・発行履歴も確認した。配置換え後は、作業と判断（対象選択時に右ペインへ詳細、中央に一覧）、Context（左サイドバーに探索、中央に編集）、検品・外部起票・利用量（左サイドバーに一覧・比較条件、利用量の表）、Issue優先度・共通の処理運用・Projectチャット・サーバーLLM接続（自動取得と「再取得」）を1440pxと1024pxで再確認した。Tauri側は未確認（共通UIとCSSは同一）。未ログイン入口と認証専用URLは固定環境では開けないため、静的描画テストで確認した。

## 実行した検証コマンド

いずれも成功。

```powershell
pnpm --filter @koujou-san/client run check   # tsc --noEmit
pnpm --filter @koujou-san/client run test    # 32 files / 118 tests
pnpm run format:check
git diff --check
pnpm --filter @koujou-san/client exec vite build   # 固定環境の dist を更新
```

## 判断を得て適用した項目（AGENTS.md 作業原則4）

表示位置・操作・取得タイミングが変わるため本人の判断を得てから適用した。いずれも既存のAPI呼び出し・要求ID・版照合・保存／LLM依頼／Run開始／外部起票の操作は変えていない。

1. 三ペイン責務への配置換え。選択した対象の詳細を右ペイン（`DetailPane`）へ: 作業と判断（`workbench.tsx`、一覧は中央に残す）、Issue優先度（`automation.tsx`）、Hopperの評価・連鎖・元結果・定義・有効化の詳細（`hopper.tsx`）、検品の判定（`inspections.tsx`）、外部起票の要求詳細（`publications.tsx`）、利用量の記録詳細（`usage-pricing.tsx`）、GitHub観測のsnapshot・履歴（`github-observations.tsx`）。一覧・絞り込みを左サイドバー（`SidebarPane`）へ: 検品定義・証拠、起票下書き・要求、保存した比較条件（`usage-comparison.tsx`）、Contextの探索（`context-explorer.tsx`、画面が表示中のときだけ描画）。Contextの編集・入力選択のプレビューは本人の判断で中央に残し、二列の `.context-layout` を廃止した。詳細ペインは `ScreenContent` の内側で描くため、非表示画面が共有ペインを埋めない。
2. 取得タイミング: Projectチャットの表示時（popoverも）にProject一覧、サーバーLLM接続の表示時に一覧、共通の処理運用の表示時に主体・処理枠・予算の現在値（取得中は入力を無効化し「現在値を取得中…」、失敗時は通知と「再取得」）、Run記録の種別select変更で即取得。手動ボタンは「…を再取得」の補助操作として残した。
3. 重複操作の削除: 外部起票の「確認する起票要求ID」欄（IDでの取得はサイドバーの「IDと版を指定して取得」に一本化）、Issue優先度の「表示」select（Triage方針・共通の処理運用ではタブにない選択肢があるため残す）。
4. 文言・ブランド: ヘッダーの副題「Software Factory Orchestra」を削除、Context画面名を「Context編集」に改名（現在位置は「Context / Context編集」）、サイドバーの画面固有一覧を主ナビの下へ移動。
5. 利用量の期間集計・export・記録・遅着訂正の一覧をJSON生表示から表（`.usage-table`）へ: 帰属・モデル・件数・数量（カテゴリごと、単位を見出しに）・金額（通貨を見出しに、推定・未算定は文字）・取得時点。数値は右揃え `tabular-nums`。表に出ない項目（反映位置、export行のkey・refs、料金版のrates、評価記録の参照）は書き出しリンクと詳細ペインに残る。
6. Context生成の変更案・候補一覧を `PageList`（100件）へ統一。結果一覧と生成待ちはサーバー固定のカーソル取得のため件数行だけ揃えた。

## 現在の判定

- 状態: Inspection pending（本人の差分確認）。
- 未確認事項: Tauri側の表示、未ログイン入口の実表示、Giteaなど実接続のある画面の実データ表示、詳細ペイン内の幅の広い表（検品条件・Run記録）の実データでの読みやすさ。
- 次の作業: commit・push・PR。
