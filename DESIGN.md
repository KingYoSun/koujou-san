# koujou-sanの視覚仕様

紙に近い背景に深緑の文字と面を置き、ライムで選択を、赤茶で注目点を示す。余白、見出し、細い罫線で情報を整理し、日本語の説明と、版・数量などの等幅表記を読み分ける。角丸と影は控えめにする。

初期リリースから膨大なContextを扱う。検索、条件による絞り込み、ページ取得、編集と確認のペインを前提にし、全件のカード表示や一件ずつの許可を標準操作にしない。小さな見本の件数を製品の件数制限と解釈しない。

説明文ではなく構造で示す。操作方法や製品の前提を述べる段落、入力のhint、値の後ろに付く補足文は置かず、見出し・札・矢印のラベル・折りたたみで伝える。状態・結果・エラー・権限不足・空表示の文だけを短く残す。

値を置くときは、誰が何を判断するかを決め、全体を示す値から内訳へ並べ、単体で判断できない値には同じ単位・同じ期間の比較対象を添える。欠測は0にせず、取得時点を付け、色だけで系列や増減を示さない（デジタル庁[ダッシュボードデザインの実践ガイドブック](https://www.digital.go.jp/resources/dashboard-guidebook) 3.2〜3.4・4.4〜4.5から採用。詳細は[数値・比較・チャート・状態](docs/design/data-display.md)）。

## 読む順番

分類・画面責務・対象と戻り先の契約は[情報構造と遷移](docs/design/information-architecture.md)を参照する。

1. [基礎](docs/design/foundations.md): 配色、書体、寸法、原本からの調整。
2. [部品](docs/design/components.md): 操作と状態の表現。
3. [三つの画面パターン](docs/design/patterns.md): Context、Run、Issueと費用。
4. [数値・比較・チャート・状態](docs/design/data-display.md): 値を読む画面の規則と出典。
5. [実装への対応](docs/design/implementation.md): 既存CSS／shadcnとの対応と確認手順。
6. [原本](docs/design/references/koujou-san-project-proposal.html)と[独立した見本](docs/design/examples/ui-patterns.html)。取得元とhashは[基礎の出典](docs/design/foundations.md#出典)を参照する。

## 適用範囲と優先順位

これはWeb／Tauriの共通UIに使う視覚仕様である。製品の意味と権限は[設計書](koujou-san-design-doc.md)と関連ADR、作業の範囲と確認は[AGENTS.md](AGENTS.md)と[Issueから実装する手順](docs/runbooks/issue-lifecycle.md)に従う。原本の企画説明から新しい機能を追加しない。

意味の正確さ、読める文字と操作、膨大なContextでの作業、視覚的な統一の順に守る。Runの実行状態と検品判定、AIの提案と採用、費用の推定と未取得を、色だけで区別しない。無注入や手動固定を省略しない。文字はサイズを問わず4.5:1以上、操作境界・focus枠・チャートの色面は隣接色と3:1以上を保つ（[デジタル庁デザインシステム カラー（アクセシビリティ）](https://design.digital.go.jp/dads/foundations/color/accessibility/)に合わせる）。

UI作業の着手時は[デザインスキル](.claude/skills/koujou-ui/SKILL.md)の確認項目に沿って進める。

見本のCSSは説明用であり、実行時のテーマの正本は既存の `apps/client/src/styles.css` に保つ。#71〜#77ではこの視覚仕様と情報構造を共通UIへ適用する。巨大なヒーローの常設、全画面のダークテーマ、ロゴの決定、全画面への印刷機能追加は含まない。
