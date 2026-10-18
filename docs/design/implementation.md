# 実装への対応と確認

[入口](../../DESIGN.md) / [基礎](foundations.md) / [部品](components.md) / [パターン](patterns.md)

## 既存CSSへの対応

確認基準はmain `1b7cb6f8f961d447ef7737329ac65ea8685614ad`。`apps/client/components.json` はshadcnの `new-york`、CSS variables有効、`src/styles.css` を参照する。現行の青系トークンと個別の画面CSSは今回変更しない。下表は今後の適用先であり、置換済みという意味ではない。

| 既存変数 | 現在値 | 視覚仕様の対応先 |
| --- | --- | --- |
| `--background` / `--foreground` | `#f5f6f8` / `#172033` | paper / ink |
| `--card` / `--card-foreground` | `#ffffff` / `#172033` | white / ink |
| `--popover` / `--popover-foreground` | `#ffffff` / `#172033` | white / ink |
| `--primary` / `--primary-foreground` | `#253c73` / `#ffffff` | green / white |
| `--secondary` / `--secondary-foreground` | `#e9edf4` / `#253c73` | wash / green |
| `--muted` / `--muted-foreground` | `#edf0f5` / `#596579` | wash / 原本muted |
| `--accent` / `--accent-foreground` | `#e9edf4` / `#253c73` | lime / green |
| `--destructive` | `#b42318` | UI調整の失敗色 `#a73629`。淡い背景との組合せは基礎を参照 |
| `--border` | `#dde2eb` | line（装飾） |
| `--input` | `#dde2eb` | UI調整の操作境界 `#77877e` |
| `--ring` | `#253c73` | green。深緑の面だけlime |
| `--radius` | `0.75rem` | 操作用3px。面は0pxを明示 |

既存の `@theme inline` からTailwind／shadcnへ流す経路を使う。原本の変数名をそのまま本番へ追加して並列のテーマを作らない。特に現行の `--radius-sm: calc(var(--radius) - 4px)` を維持したまま基底を3pxにすると負値になるため、適用作業では派生値も合わせて変更する。表は値の機械置換手順ではない。

実行時の正本は既存の [styles.css](../../apps/client/src/styles.css)。見本のinline CSSは文書の図示だけを担い、アプリからimportしない。新しいテーマパッケージ、フォントCDN、UIフレームワーク、Storybookを導入しない。

WebとTauriは同じReact UIとCSSを使う。OS連携・CLI実行はネイティブに保ち、見本や見た目の変更でWebへ実行能力を加えない。操作の可否・表示する状態は既存の契約から受け取り、色や部品の都合で変更しない。

## 今後の適用順序

画面の実装依頼ごとに、対象と受入条件を閉じてから適用する。まず既存変数と文字・余白を共通CSSへ対応させ、対象画面の部品を既存shadcnと共通UIへ寄せ、不要になった個別スタイルを同じ作業で削除する。次に三パターンのうち対象のペイン・一覧・状態表示を確認する。一括リデザインや新旧テーマの併存を今回の完了条件にしない。

初期値・操作・表示内容が変わる適用ではAGENTS.mdの判断確認に従う。この文書の例だけを、権限や自動処理を変える承認として扱わない。

## 見本を開く

ファイルをブラウザへ直接開ける。HTTPで確認する場合はrootで次を実行し、表示されたポートで原本と見本を開く。静的ファイルだけを配信し、API・CLI・CIは開始しない。

```powershell
python -m http.server 8470 --bind 127.0.0.1 --directory docs/design
# http://127.0.0.1:8470/references/koujou-san-project-proposal.html
# http://127.0.0.1:8470/examples/ui-patterns.html
```

見本のフォーム・タブはページ内の表示だけを変える。更新・外部送信・保存を行わない。印刷はブラウザの印刷から確認する。

## 確認範囲

原本のhash、文書リンク、例の状態名と既存仕様、JavaScript構文、差分を確認する。原本と見本を390／768／1440px幅で開き、文字切れ・重なり・ページ全体の横スクロールを観察する。見本ではTab・矢印・Home・End・Enter、focus-visible、無効操作、通常文字と状態表示の色の組合せを確認する。

200%拡大時の読み順・操作と、印刷時の内容・状態の欠落も確認する。資料の印刷ではナビなどの操作部品を省けるが、回答・版・由来・状態は残す。原本の欠点は改変せず記録し、UI調整と混同しない。ブラウザ版、対象hash、画面幅、実測値、未確認事項は[検証記録](../progress/2026-10-19-issue-47-evidence.md)へ残す。
