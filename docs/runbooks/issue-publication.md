# 外部Issueの起票

Workspaceの「外部Issueの起票」から、端末の下書き、共有下書き、送信前プレビュー、共有受付、外部作成の確認を操作する。保存だけでは起票しない。結果不明の要求を再作成せず、「既存Issueと照合・外部内容を再取得」で確認する。

## 書込み接続の設定

サーバーへ`KOUJOU_PUBLICATION_CONFIG`でGit管理外のJSONファイルを指定する。例は次のとおり。実際のApp・installation・repository IDとWorkspace IDへ置き換える。秘密鍵や実設定をリポジトリへ追加しない。

```json
{
  "app_id": "YOUR_APP_ID",
  "private_key_file": "C:/Users/kgm11/koujou-san/.cache/github-publisher/private-key.pem",
  "connections": [
    {
      "id": "project-issues",
      "workspace_id": "YOUR_WORKSPACE_ID",
      "repository": "KingYoSun/koujou-san",
      "repository_id": 1,
      "installation_id": 1
    }
  ]
}
```

Appには対象repositoryのIssues読取り/書込みとMetadata読取りを設定する。公開Webhookは不要。観測用Appの設定をそのまま書込みへ広げない。接続が未設定なら、共有下書きを保存しても起票要求は受け付けない。CLI認証・個人tokenを共有設定へコピーしない。

## 操作

1. 起票下書きを新規作成し、名前、設定済みの起票接続ID、GitHubのタイトル・本文を入力する。必要な固定参照だけを参照元へ指定する。
2. 共有へ保存する。端末の下書きと共有の前提版が異なる場合は比較し、必要なら取得した共有内容から編集し直す。
3. 固定下書きから送信内容をプレビューする。本文末尾の照合用HTMLコメントを含め、対象repositoryと実際に送信する内容を確認する。
4. 実行を許可されたメンバーが「この固定内容で起票を依頼」を押す。共有受付は外部の作成確認とは別である。
5. 要求の状態を取得する。結果不明なら同じ要求のまま既存Issueを照合する。検索で見つからない場合も、未作成と断定して再送しない。

切断中の入力は端末に残り、再接続しても自動送信しない。送信案には要求IDと操作IDを保存する。再操作では現在の共有版と既存要求を確認する。Workspaceへのアクセス失効・ログアウト時は既存の下書き削除方針を適用する。

外部作成を確認した要求では、外部識別子、取得したタイトル・本文・状態、外部更新日時と取得時刻を表示する。外部で手動編集された内容を共有下書きで上書きしない。共通Runが通信失敗で終了していても、その後に既存Issueの作成が確認される場合がある。元のRun結果と現在の外部確認を混同しない。

## APIと検証

入口は`/publications/workspaces/{workspace}`。`records/{id}`の取得/下書き保存、`preview/{request-id}`、`submit/{request-id}`、`refresh/{request-id}`、`query`を使う。下書きIDは`publication-draft:*`、要求IDは`publication:*`。保存・受付は共通の`operation_id/expected_version`を指定する。refreshは対象の固定`version`を指定し、作成処理を再実行しない。

queryのheadsは種類別のメタデータ、historyは選んだ所有対象の固定範囲を返す。続きはnextをafterへ渡し、履歴ではthroughも保持する。最大100件のページで取得し、全Context・全履歴を通常操作で読み込まない。

`pnpm run test:publications`は専用PGと固定GitHub HTTPで、受付、同時送信、応答喪失、プロセス消失、照合、現在認可、外部編集、確定失敗を確認する。実Oryと共通画面は`pnpm run test:mcp:ui`を使う。固定接続試験と実GitHubへの作成確認は別に記録する。

実接続は、明示した`KOUJOU_PUBLICATION_CONFIG`と`pnpm run test:publications:live`を使う。初期の検証先は許可した`KingYoSun/koujou-san`だけ。所有する合成Issueを一件作成・再取得し、既存の`gh`認証でそのIssueだけを編集・Closeする。実際のApp・対象への許可を得る前には実行しない。通常のCIではこの明示実接続試験を実行しない。
