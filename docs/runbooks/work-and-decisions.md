# 作業と判断

Workspaceの「作業と判断」から、Run・工程実行の一覧と詳細を開く。目的、待機理由、担当、次の操作を確認し、選択した作業の固定成果物・質問・検品条件・証拠へ進む。一覧はページ単位で取得し、最終同期時点の表示と、通信断による古いキャッシュを区別する。

## 質問へ回答する

1. 対象の作業を開き、質問、背景、確認する操作、対象版、選択肢と影響を読む。
2. 回答案は端末へ保存される。再接続だけで送信されない。必要なら現在の作業・質問を更新する。
3. 「回答を送信」で固定した質問への回答を依頼する。API受付、Runner配送、継続確認は別の表示である。判断不能も理由付きで残せる。

HumanReviewRunも同じ入口を使う。`request.response_format`が`verdict`の質問は、判断・理由・確認した証拠を表示する。`evidence_required`がtrueなら証拠を入力してから送信する。質問本文の原文も展開して確認できる。既存の自由文・任意JSONの回答も維持する。

## 回付と差し戻し

「判断依頼を担当者へ回す」では、現在回答できるWorkspaceメンバーを選び、理由を保存する。相手は「自分に回された判断依頼」で取得できる。回付の保存は、相手が読んだことや回答したことを意味しない。指名だけで権限を付与せず、回答できる人は既存の編集権限に従う。

「理由付き差し戻しを記録」では、対象成果物と固定版、該当条件、観測した問題、期待する結果を保存する。元の入力にある成果物を初期表示し、必要な対象と版を確認して保存する。修正・再検品は、既存の新Run作成で関連元を指定する。作成だけでは起動しない。「関連する修正・再検品Runを確認」と「関連元のRun・判断を確認」で前後へ移動できる。

## APIと検証

入口は`/workbench/workspaces/{workspace}`。`POST /query`の`kind=work`はRun・工程のメタデータ、`kind=notes/history`は固定`subject`の補足記録を返す。`after`と`next`でページを進める。`GET /input/{run-id}?version=...`は成果物・派生契約・関連元・固定参照だけを返し、Contextとprompt本文は返さない。

`POST /notes/{work-note-id}`は共通の`operation_id/expected_version`と`note`を使う。`kind=route`はsubject、任意のpause固定参照、recipient、reason。`kind=return`はsubject、reason、targets、criteria、problem、expected。回答・実行のAPIは増やさず、既存のRun/Pause・工程の入口を使う。

`pnpm run test:workbench`は専用PGで保存・ページ・現在権限・二人目の回答・差し戻しと新Runの関連を確認する。`pnpm run test:workbench:ui`は実Ory・二アカウント・1万Context×3版と所有CLIプロセスを使う専用画面を起動する。試験用接続は接続更新を維持し、配送・継続報告を所有fixtureの操作で分けて観測できる。標準の開発サービスや認証設定は変更しない。
