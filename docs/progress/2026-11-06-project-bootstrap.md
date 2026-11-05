# 初期ContextとProjectの作成

## 範囲

直接の依頼に基づく。基準commitは `253675026311e314636bfb0cce949aa27284c8a9`、開始時の作業ツリーに変更なし。
リポジトリとWorkspaceを作った後、Contextがない状態から製品の画面だけでProjectを作れるようにする。
初期Contextは入力本文をそのまま文（Sentence）として保存する。LLMによる要約・分割、既存Projectへの一括取込み、リポジトリ自動作成は対象外。

## 条件

| ID | 対象・期待結果 | 判定方法 |
| --- | --- | --- |
| AC-1 | 共有版がないProject・Contextの下書きを編集し、既存の共有保存操作で保存できる | 新規下書きの描画・編集・保存の回帰テスト |
| AC-2 | Project名と自由入力、または `.txt`・`.md`・`.html` の本文から初期Context、Project、直接所属を保存できる。本文を保持し、既存Context・親Scope・LLM接続・Runを要求しない | 空の保存先からフォームを操作し、保存本文・所属とProject固定版を照合 |
| AC-3 | 作成結果からProjectチャットへ進める。保存失敗を成功と表示せず、同じフォームでの再確認は既存の要求IDを再利用する | 結果リンクと応答喪失後の再確認テスト |
| INVAR-1 | 既存の編集権限、版管理、下書きと結果不明の保存契約を維持する | 閲覧専用・既存版のテストとSyncEngineの回帰テスト |
| INVAR-2 | Workspaceの全Contextを取得せず、今回作る対象だけを扱う | 通信経路と取得対象の確認 |

## 画面と実装

1. Workspaceの編集者がProject名・入力本文と作成結果を確認する。
2. 中央にProjectと初期Contextの入力・結果を置き、既存のScope一覧は左に保つ。
3. Project作成の入口にテキスト／ファイル入力を加える。既存の空Project作成と文書編集は維持する。保存形式・既存APIを変更しない。

新規下書きの編集・保存は既存コードで対応済みだった。変更前の回帰テストも成功した。今回の不足は、自由入力・ファイルから初期ContextとProjectをまとめて作る入口だった。
Context画面へ初期作成フォームを追加し、既存SyncEngineの作成・保存と認可を使った。新規下書きの条件とProjectの所属契約を確認し、調査を終えた。

## 検証

区分B。既存保存経路を使うUI追加であり、API・保存契約・認可は変更しない。固定差分に対する独立検品も行う。
関連UIテスト、共通UIの型検査・テスト・build、整形と差分検査、1440px／1024pxの実表示を確認する。
実APIを使わない検証は合成データによる検証と明記する。

## 現在の判定

Ready。依頼範囲のローカル実装、検証、独立検品を完了。commit・PR・配布は今回実施していない。

| 条件 | 結果と証拠 |
| --- | --- |
| AC-1 | 新規Scope／Sentenceの描画テストと既存編集テストが成功。既存実装を維持 |
| AC-2 | `project-bootstrap.test.tsx` で自由入力・txt・md・htmlの本文保持、Project、所属の保存を確認。`test:forge` の実API／専用PostgreSQL／Reactフォームでも空のWorkspaceから作成し、本文をAPIから再取得 |
| AC-3 | 実API試験で作成後にリポジトリ設定・チャット作成へ遷移。合成HTTPでは応答喪失後に同じ要求IDで再確認。独立検品で見つかった下書き閲覧後の再開失敗は、確認済み固定版の保持で修正。追加テストの変更前の失敗と修正後の成功を確認 |
| INVAR-1 | 閲覧専用では保存不可。既存SyncEngineの保存・版・結果不明処理を再利用。非表示時はフォームをDOMから外し、再訪時には入力を保持 |
| INVAR-2 | 新規3対象だけを作成・同期。Workspace全件の取得を追加していない |

実行した確認：

- `pnpm --filter @koujou-san/client run check`：成功。
- `pnpm --filter @koujou-san/client run test`：最終修正後155件成功、専用環境が必要なセットアップ試験1件は通常実行時skip。
- `pnpm --filter @koujou-san/client exec vitest run project-bootstrap.test.tsx context-editor.test.tsx sync-engine.test.ts`：最終修正後24件成功。
- `pnpm run test:forge`：成功。専用PG、実Go API、管理下のOAuth／Gitea HTTPとReactフォームの試験を含む。通常実行でskipされるセットアップ試験もここで成功。
- `pnpm --filter @koujou-san/client run build`：成功。500kBを超えるchunkの警告あり。
- `pnpm run format:check`、`git diff --check`：成功。

Chromeの合成データプレビューで1440×1000／1024×1000を実操作した。ページの `scrollWidth/clientWidth` はそれぞれ1425/1425、1009/1009。文字の重なり、focus枠の欠け、横スクロールなし。作成結果と初期Contextの編集リンク、Projectチャットのリンクを確認。色・CSSの追加なし。結果画像は `.cache/project-bootstrap-preview/result-1440.jpg`。

Tauriの実操作とインストール済みアプリへの配布は未実施。Web／Tauri共通のUI変更であり、ネイティブ処理の変更はない。実API試験は通常UIの操作とAPI読取りで検証し、補助スクリプトやDB直接更新による初期データ投入を受入証拠にしていない。

独立検品は実装の会話履歴を継承しない別担当で実施し、6ファイルのSHA-256を固定した。初回の指摘1件を上記AC-3の再現テストで確認して修正し、更新hashで再検品した。関連24テスト、型検査、差分検査が成功し、指摘の解消とblockerなしを確認した。実APIと実表示は主担当の証跡を使い、独立検品での実行と区別した。
