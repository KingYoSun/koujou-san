# Issue #7の検証記録

- 対象: Issue #7、Scope revision 1、区分C。
- 基準: `491ecd9c6a6640f0f0d857a21069c848019eaf23`。
- 現在の判定は[現在状態](current-state.md)へ集約する。

## 条件と観測

| 条件 | 検証と結果 |
| --- | --- |
| AC-1 | 実PGのHTTP APIで四粒度を単独保存・取得し、共有子の型・順序・版と、固定した外部の一記述・添付・由来を保持。型違い・参照切れ・循環・head参照は登録/解決で拒否 |
| AC-2 | 四粒度をそれぞれproduct/project/milestone/issueへ所属させた。親の所属解除は子・他Scope・本文へ影響せず、解除前の所属版を取得できた |
| AC-3 | 子の新版を保存しても既存親の内容は一致したまま。各親を明示更新した場合だけ新版の参照を採用した |
| AC-4 | Scopeの所属と既定候補を持つ基準でもnone・未指定は入力を省略。直接値とnoneの併存をselection_conflictとして拒否 |
| AC-5 | Go/TSで見出し・段落・共有子の2配置を再構成。親子の重複した直接選択を整理し、全経路のexcludeで抜粋と空コンテナ除去。異版を自動採用せず衝突を返した |
| AC-6 | 共有更新前後で旧基準によるdigestは一致。明示採用で旧基準・変更候補・影響Runと新基準を記録。新Manifestのみ入力が変わり、開始済みManifestの保存内容は不変 |
| AC-7 | 外部未取得・権限不足・容量超過・部分候補を別の不足として返し、完全なManifestを保存しなかった。16 MiB・10万配置と境界値、設定変更・不正設定も確認 |
| AC-8 | 直接値・元outputの版/digest/箇所と原内容を保持。pause追加情報は元Manifestへの別記録で、開始時inputを変更せず読了はunobserved |
| INVAR-1〜5 | 文書・所属・入力の保存先と操作を分離。公開親でも権限のない子を取得・解決できず、その子を含むManifestも拒否。本文編集で制限付き権限を共有へ戻さず、所属取消後の取得も拒否 |

実DB試験は`pnpm run test:contexts`で成功した。`pnpm run check / test / build`、`test:database / test:access / test:auth`も成功した。通常UI/契約は4ファイル14件。既存PGの停止・復元と実Oryの登録・復旧・TOTP・失効を維持した。GoとTypeScriptの契約fixtureは同じ入力文字列とdigestを照合する。Context UIは本Issueの対象ではなく、共通業務APIをHTTPから実操作した。

## 調査・失敗と修正

CodeGraphを先に使って既存の保存・共通認可・起動経路を確認した。新規contexts/TS契約ファイルは索引に見つからなかったため実ファイルへ切り替え、索引を新規作成していない。

最初のTypeScript型検査はassertion関数の宣言形式で失敗した。型を絞り込める関数宣言へ修正し、型検査と14件の通常UI/契約テストが成功した。

## 接続範囲と残作業

正式schemaと扱いは[ADR 0006](../adr/0006-context-manifest.md)、入口は[手順](../runbooks/contexts.md)を参照する。直接値の由来は提供者が明示した値を保存する。実Runの生成元照合・配送、実検索エンジン、編集GUIを実装済みとは扱わない。

[独立検品](2026-10-15-issue-7-inspection.md)は全条件が適合、blockerなし。PR CIとマージ後照合を残す。#7ではネイティブ実装・通常画面の挙動を変更していないため、ローカルでdesktopコマンドは再実行していない。PRで発生するWindows CIは確認する。
