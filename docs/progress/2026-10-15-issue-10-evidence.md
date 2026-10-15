# Issue #10の実装・検証記録

対象と数値は[計画](2026-10-15-issue-10-plan.md)、現在の判定は[current-state.md](current-state.md)へ集約する。基準は`730f20ff9ae3c0bf4dcf2dc1d12270796c4a93e3`。接続とデータモデルは[ADR 0009](../adr/0009-arcadedb-graph.md)、実行方法は[関係探索の手順](../runbooks/graph.md)を参照する。

## 受入条件との照合

| 条件 | 実接続で確認した結果 |
| --- | --- |
| AC-1 | Go→ArcadeDB→HTTPで四粒度の固定版、共有Sentenceの重複配置と順序、2 Scopeへの独立所属、宣言済みRunのuses/generatedを取得した。Runの次版を保存しても、以前の固定Run版から由来を辿れ、元の関係要求の再送結果も変わらない |
| AC-2 | 関係とcheckpointを同じnative transactionへ保存した。commit前のcheckpoint書込み失敗では全体をrollbackし、再試行で反映した。実commit後の応答をproxyで失わせ、保存済み位置の再照合で成功へ回復した。重複・古い位置、同一batchでの所属更新・解除、Context削除の収束を確認した |
| AC-3 | 深さ・ノード・関係・ページの上限、実応答のtruncated化と完了Statsの切除、期限切れ、投影待ちを再現した。いずれも探索からManifestを保存せず、保存済みManifestの本文・digestは変わらない。ArcadeDBの実停止でも新規確定を拒否した |
| AC-4 | 520件を追加してsnapshotを複数commitに分けた。後続batchの失敗後もloading世代のロード済み件数とsnapshot位置が残り、既存activeを維持した。再試行中の本文・所属解除・採用済み関係を差分で取り込み、件数検証後に切り替えた。native backupを別DBへ復元し、HTTP応答のID・版・関係・Origin・位置が一致した。実コンテナの停止/再起動と、正本の履歴不足からの再構築も通った |
| AC-5 | 同じIDを持つ2 Workspace、3 User、制限付き共有Sentence、権限取消を使った。権限外の子・関係・固定版を返さず、別Userのcursorを拒否した。継続は現在権限を再確認し、全ページを重複なく取得した |
| AC-6 | 下記の固定データ・設定・Windows環境で全数値目標を満たした。先頭ページの応答を測定し、ページ継続の機能はAC-3/5で別に確認した |

Runは既存の共通保存契約を使う宣言fixtureであり、実RunやCLIの実行成功を意味しない。MCP公開・共通GUIでの探索操作は後続Issueの担当とし、この記録ではHTTP APIとContext解決の接続を確認した。native RID、LLMの再推論、本文検索の移転は導入していない。

## 合意負荷

Windows/amd64、AMD Ryzen 7 7800X3D、Docker 29.5.2のLinux VM（4 CPU・約15.6 GiB）で測定した。ArcadeDB 26.10.1、Go HTTPドライバ0.2.0、Go 1.27.2。試験専用コンテナは1 GiB上限、実JVMのMaxHeapSizeは512 MiB、page cacheは16 MiB、WAL flushは1とした。標準Composeにも同じheap/cache/WAL設定を反映した。

生成データはSentenceのContext 1万件×3版、Scope 100、宣言済みRun 1千、型付きの正本関係20万。共有Sentenceへの参照5千、深さ8・分岐20を含む。Contextの他の三粒度とScope所属はAC-1の機能試験で確認した。

4クライアントが、versionの外向き探索、参照集中の逆向き探索、深さ8・分岐20のreference探索、Runのuses/generated探索を各20回、合計80回HTTPで行う。同時に4更新者が各10回の関係を保存し、workerによる反映を照合する。探索は既定の深さ・展開上限と1ページ100件を使う。外部Ory認証の時間は測定へ含めない。

実DBファイルは281,138,092 byteで、設定cacheの16 MiBを超える。実コンテナを永続volumeのまま再起動し、空のArcadeDB page cacheから同じ4探索を合計40回測定した。OS全体のfile cacheは消去していない。全サンプル・JVM flags・メモリpeak・I/Oは[測定JSON](2026-10-15-issue-10-load.json)に残した。

| 指標 | 目標 | 最終実測 |
| --- | --- | --- |
| warm p95 | 500 ms以下 | 107.498 ms |
| cache外・再起動後p95 | 2,000 ms以下 | 172.461 ms |
| 反映遅延 | 5,000 ms以下 | 最大1,128 ms |
| 再構築 | 300,000 ms以下 | 115,590 ms |
| ArcadeDBコンテナのメモリpeak | 1 GiB以下 | 1,014,312,960 byte |

メモリpeakは再起動前後の大きい方を使い、JVM heapだけを報告していない。再起動前のcgroup I/Oはread 8,192 byte・write 851,636,224 byteだった。これはコンテナへ計上された物理I/Oであり、DBの論理読取り件数やホスト全体のI/Oとは区別する。再起動でcgroupの計数がリセットされるため、前後の値を分けて記録した。

初回はwarm p95 847.526 msで未達だった。SoTの過去Run参照を照合するSQLへ索引を追加し、一度は432.813 msとなったが、最終確認で1,034.022 msへ悪化した。達成扱いを戻し、先頭100件を返す前にも上限近くまで探索していた処理を改めた。幅優先の安定した発見順で次の1件まで取得して区切り、継続では必要な先頭範囲を再探索する。データ数・初期上限・目標値を変えず、最終実測で達成した。

## 検証コマンド

`pnpm run test:graph`と`pnpm run test:graph-load`は試験専用PG・ArcadeDBで成功した。機能試験の最終実行は`TestGraphPostgres` 14.98秒、負荷試験は`TestGraphLoad` 143.49秒。標準の開発用volumeは削除していない。

Go全体テスト、`pnpm run check / pnpm run test / pnpm run build`は成功した。共通UIは5ファイル24テストが成功した。共通認可・既存cursorを変更したため、実PGの`test:access / test:contexts / test:search`も成功した。CI・固定commitの独立検品・マージ後の判定は、完了後に追記する。

CodeGraphで既存のContext解決・認可・API入口・検索利用側を確認した。新規graphindexは未索引だったため、その範囲は直接読んだ。新しい索引は作成していない。日本語文書はyomiyasuで推敲し、製品内で使う「正本」「固定版」などの用語は保持した。

試験用PGの初期化が60秒を超え、最終serverへの切替前に接続した失敗も記録した。試験用の起動期限を標準と同じ180秒へ合わせ、最終PID 1がpostgresになりTCPで応答するまで待つよう共通runnerを修正した。設定や耐久性を弱めず、実PGの認可試験を再実行して成功した。Context・検索の実PG回帰も同じ入口で成功した。
