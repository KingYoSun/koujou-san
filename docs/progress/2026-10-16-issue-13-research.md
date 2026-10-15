# Issue #13の先行調査

2026-10-16。#12のCI待ちに実施した読み取り調査。製品Connectorの実装・受入試験は未実施。

## 固定する範囲

Issue #13 Scope revision 1のAC-1〜6・INVAR-1〜2を維持する。既存Issue/PR/commit/Actionsを参照・対応付けてサーバーから観測する。PR作成、merge、CI起動、観測ごとの業務Run作成、Workflowの自動前進は対象外。

認証方式、定期照合間隔、保持方針はユーザーへ選択肢と影響を提示して回答待ち。判断に依存する実装やADRの採用は行っていない。

## 実GitHubで確認した例

- repository ID 1025894008、kukuri-app/kukuri、public、default branch main。
- Issue #1220はclosed。PR #1843のmerge commitはdf9cb5e56114ccc5c9f8f5976a2945f0ffeac799。
- Actions run 38784153986、workflow 248011545、head SHA ec3a86cf6a02bfd97079fd7a2e918afd06096c3c。attempt 1はfailure、attempt 2はsuccess。
- attempt 2のjobsは28件、3件がskipped。ここでは必須ジョブかどうかを推測していない。固定した必要ジョブと各回の結果を照合する試験例に利用できる。
- 読み取りは既存ghのGETだけで行った。資格情報を製品へコピーしていない。この調査は、専用read-only資格情報の最小権限や製品のWorkspace境界を検証した証拠には数えない。

## 認証とAPI制約

GitHub公式資料では、共有・長期運用にはGitHub Appを推奨する。installation tokenは対象repository_idsとpermissionsを縮小して発行でき、1時間で失効する。PATは発行者のアクセス喪失で無効になる。

GitHub App installationの標準枠は最低5000要求/時。未認証は60要求/時なので、公開kukuriを読めることだけを継続観測の認証方式の根拠にしない。正しく認証した条件付きGETが304ならprimary rate limitに算入されない。ETag、ページ継続、API残量とRetry-Afterを利用する。GitHubはAPI要求の直列化を推奨している。

Webhookは現在状態の再取得を促す入力として扱う。受信payloadで共有状態を上書きしない。受信後10秒以内の2xx、重複delivery ID、遅延・逆順の通知を試験対象にする。公開Webhook入口のない配置でも定期照合を利用できるようにする。

## 実装へ進む際の確認点

API登録はapps/server/cmd/api/main.go、共通の現在認可はaccess.Check/CheckTx、保存はpersistence.Storeと追加migrationを利用する。認証の秘密値を共有mutation・同期・ログへ含めない。

大量Contextを巡回して外部参照を探さず、登録した外部対象とrepository/実行IDの索引から処理する。複数Contextの同一対象参照をまとめ、ジョブはattempt別にページ取得する。通常表示・履歴・同期をWorkspace全体の本文取得へ接続しない。

失敗した取得では最後の成功観測と時刻を保持し、現在の観測可否を別に示す。CI conclusion、観測処理の成否、工程の受入判定を分ける。途中で失敗したページ集合を完全な観測として確定しない。

## 一次資料

- https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/deciding-when-to-build-a-github-app
- https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app
- https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens
- https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api
- https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api
- https://docs.github.com/en/rest/actions/workflow-runs
- https://docs.github.com/en/rest/actions/workflow-jobs
- https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks

資料はFirecrawlで取得して照合した。JSONは.cache/issue13-investigationへ保存した公開データである。
