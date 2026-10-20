# Issue #47: 視覚仕様と参照見本

依頼は[Issue #47](https://github.com/KingYoSun/koujou-san/issues/47)。Scope revision 1、区分A。目的・対象外・AC-1〜7・INVAR-1〜4・調査の終了条件はIssue本文を再利用する。本番UIの変更、全画面実装、新しい開発フローは対象外。

## 開始時の状態と作業

基準mainは `1b7cb6f8f961d447ef7737329ac65ea8685614ad`、開始headは前作業の完了記録だけを含む `552fde9`。`codex/issue-47-design-system` で作業する。#33・#34は本人の指定どおり保留し、実接続の承認枠は消費しない。

Issueの原本をhash照合し、DESIGN.md、基礎・部品・三パターン・実装対応、独立HTML見本を追加する。現行CSSは青系のままで、原本との対応を文書化する。存在しないADR 0014へのAGENTS.mdの参照を、既存の作業手順へ直す。新たなADRは追加しない。

コード理解の入口としてCodeGraphを使ったが、`apps/client/src/styles.css` は索引対象として見つからなかったため直接読みへ切り替えた。索引は変更していない。区分Aのため別担当の監査や全製品テストは必須化せず、対象の構文・参照・実表示と実際のPR CIを確認する。

## 受入条件との照合

| 条件 | 確認対象と方法 | 結果 |
| --- | --- | --- |
| AC-1 | 原本の取得元・日付・bytes・SHA-256 | 74,390 bytes、Issue指定hashと一致。基礎に出典を記載 |
| AC-2 | DESIGN.mdから全資料、AGENTS.mdとdocsの導線 | ローカル文書リンク72件とHTML内リンクを確認、欠落なし |
| AC-3 | 原本値・UI調整・追加状態色の区別、見本 | 基礎・対応表・見本が一致。コントラストの実測は下記 |
| AC-4 | 既存CSS/shadcn対応とWeb/Tauri、適用順、差分 | 本番CSS・コード・設定の変更なし。派生radiusの注意も記載 |
| AC-5 | 七種の部品の状態表と三つのパターン | 無注入・判断不能・手動固定・推定・未取得を文書と実表示で照合 |
| AC-6 | 原本・見本の390/768/1440px、キーボード、色、200%・印刷 | 下記の方法・範囲で確認。ChromeのズームUI・印刷出力は未確認と区別 |
| AC-7 | 既存仕様、リンク、例、構文、git diff --check | リンク・両HTMLのIDとJS構文、差分の空白検査が成功 |

INVAR-1〜4は本番・API・保存形式の差分がないこと、見本に通信・実行がないこと、ページ取得を前提にした説明、業務状態の区別で確認した。新規ファイルは文書と参照見本だけ。Rig・Bifrostなどの採用判断を企画HTMLで置き換えていない。

## 表示と操作

WindowsのChrome `155.0.8059.39` をChrome拡張経由で操作した。静的HTTPは `127.0.0.1:8470`。製品API、Tauri、CLI、外部LLMは起動していない。見本は16,323 bytes、SHA-256 `1779e77ba5a44474e0d39daed67ae90c365739e99d1d43975b9d5ff764b68642`。

| 対象 | 指定幅 | ページのclientWidth / scrollWidth | 結果 |
| --- | --- | --- | --- |
| 原本 | 390 / 768 / 1440px | 375/375、753/753、1425/1425 | 意図しないページ全体の横スクロールなし。本文とContext部分の描画・折り返しを観察 |
| 見本 | 390 / 768 / 1440px | 375/375、753/753、1440/1440 | 狭い幅で一列、広い幅で二ペイン。文字切れ・重なりなし |

clientWidthとの差15pxは縦スクロールバー。DOM全体の、表示中でoverflow hidden/clipかつ内容幅が枠を超える要素も両HTMLで0件だった。原本の巨大な全ページ画像は取得タイムアウトとなり、通常の画面画像とDOMの幅・切り取り検査へ切り替えた。原本は低密度の企画資料であり、細かい文字サイズをUIの標準値にしていない。

見本の左右・Home・Endで注入方法を切り替え、aria-selected、roving tabindex、対応パネルの名前、無注入0件の本文を確認した。Tabでパネルへ移り、focus-visibleの深緑枠を確認した。回答欄へ合成入力し、Enterでページ内の完了通知を確認した。定義保存の見本もページ内通知だけを表示し、Run開始ボタンは無効のままだった。原本のタブでも初見の検品＝context: noneを確認した。

### 色の実測

描画されたHTML文字のcomputed colorと祖先の背景を合成し、sRGB相対輝度から比を計算した。見本は14組、原本は49組。以下は本文・補足・操作・状態を含む代表値で、通常状態の見本の最小値は節番号の4.75:1。

| 前景 / 背景 | 比 | 対象 |
| --- | --- | --- |
| ink / paper | 12.31:1 | 見本本文 |
| ink / white | 13.32:1 | 見本の面と操作文字 |
| muted / paper、white、wash | 5.37、5.81、4.96:1 | 補足・メタ情報・無効ラベル |
| white / green | 12.14:1 | 主操作・選択タブ |
| white / green2 | 8.59:1 | 主操作のhoverを実表示で確認 |
| control / white | 3.75:1 | 入力欄の識別に必要な境界 |
| green / lime | 9.80:1 | 選択・採用済み |
| green2 / wash | 7.34:1 | 同期済み |
| warning / peach | 6.31:1 | 質問待ち・不完全・競合 |
| error / error-bg | 5.66:1 | 取得失敗 |
| orange / paper | 4.75:1 | 節番号 |

原本の薄緑の矢印 `#839383` / `#eef0e6` は2.82:1、節番号07のorange / `#e9ede2` は4.36:1（23px・400）だった。原本は変更せず保存し、これらをUI用の文字色の根拠にはしない。状態は文字・記号と併用し、lineを操作境界に使わない。

### 200%と印刷

拡大は一時的な別HTMLのiframeを `transform: scale(2)` で表示し、1440pxの外枠に720 CSSpxの内容を2倍で描画した。原本・見本とも内側のclientWidth / scrollWidthは705/705。原本の無注入切替、見本の一列配置・無注入切替・回答入力とEnter通知を確認した。Chromeのズームショートカットでは寸法が変わらなかったため、**ChromeのズームUI自体を確認したとは扱わない**。原本・見本のファイルは拡大のために改変していない。

印刷ボタンのクリックは印刷ダイアログ待ちとなり、Chrome拡張からプレビューを取得できなかった。検証タブだけを閉じて復帰した。代わりにWSLの一時保存先へWeasyPrint 70.0とPyMuPDF 1.28.2を導入し、原本と見本の印刷CSSをA4 PDFへ描画した。製品の依存は増やしていない。

原本14ページ、見本5ページを画像と抽出文字で確認した。本文・固定版・由来・状態・回答例・費用表に欠落はなく、ページ外の文字ブロックは0件。見本は現在の初期表示を出力し、非選択の無注入パネルを重ねて出力しない。原本には余白の大きい改ページがあるため、見本は一列と内容単位の改ページを使った。これは**WeasyPrintによる静的な印刷CSSの確認**であり、Chrome固有の印刷結果や操作後の動的入力を確認したとは一般化しない。一時PDF・画像・拡大枠・検査スクリプトはGit管理外に置いた。

## 局所検証

原本hash、Markdownリンク72件、HTMLの重複ID・内部リンク、両HTMLのinline JavaScriptの構文を一時スクリプトで確認した。見本のスクリプトに通信・保存・プロセス起動はない。`git diff --check` は成功。日本語はyomiyasuで推敲し、箇条書きや技術用語に対する機械的な候補は文書の用途に沿って判断する。

## CIとマージ後の照合

[PR #68](https://github.com/KingYoSun/koujou-san/pull/68)の最終headは `81372688549d5a7b95a87e637b9750ab0bbfedd9`。[CI 39145167276](https://github.com/KingYoSun/koujou-san/actions/runs/39145167276)はattempt 2で成功した。

初回の共有側は `TestOryIntegration` で `listen tcp 127.0.0.1:38341: bind: address already in use` となった。CodeGraphで `auth_test.go:74〜78` を確認し、検証APIのlisten時の失敗と特定した。Windows実行中は再実行を受け付けなかったため完了を待ち、失敗した共有側だけを同じheadで再実行した。ポート競合は再発せず、コードの変更や試験の省略は行っていない。

| チェック | 結果 |
| --- | --- |
| [Shared UI and server](https://github.com/KingYoSun/koujou-san/actions/runs/39145167276/job/117730898248) | success / 10分43秒 |
| [Windows native client](https://github.com/KingYoSun/koujou-san/actions/runs/39145167276/job/117730899755) | success / 15分18秒 |
| GitGuardian Security Checks | success |

全チェックとhead一致を確認してマージした。merge `35995f473e58ee0d27399acaa60d1ebf9ae3cd79` と最終headのtreeは `3a5d6f9a8629cc6fa38b14cf30699b78691d2c69` で一致し、`git diff --exit-code` は差分0。mainへfast-forward後も原本hash・リンク・構文を再照合し、#47をCloseした。現在の判定は[current-state](current-state.md#issue-47の判定)を参照する。
