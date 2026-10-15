# Issue #10の独立検品

## 対象と入力

- 最終対象: `e3c25ccfb1eb31e980a6501aa27d645616244662`。初回対象`578639535c4be1588489d1fc4014f5fa30c1ad88`から、fixture表記の差分も再確認した。
- 基準: `730f20ff9ae3c0bf4dcf2dc1d12270796c4a93e3`。Issue #10、区分C、Scope revision 1。
- 検品者: freshコンテキストの`issue10_independent_review`、2026-10-15。
- 入力: AGENTS、開発・Issue手順、Issue本文、[計画](2026-10-15-issue-10-plan.md)、設計書、[ADR 0009](../adr/0009-arcadedb-graph.md)、[検証記録](2026-10-15-issue-10-evidence.md)と測定JSON、固定commitの実装・試験。
- 実装者の会話履歴・自己評価は継承しなかった。既存devの資格情報を出力・複製せず、専用Docker projectとvolumeで試験した。

## 条件ごとの結果

| 条件 | 確認した経路と結果 | 判定 |
| --- | --- | --- |
| AC-1 | 四粒度・共有子・配置順・独立所属と固定Run版の利用/生成由来を、SoT・投影・HTTPで照合 | PASS |
| AC-2 | 同一transactionでデータとcheckpointを確定。重複・古い位置、commit前rollback、commit後の応答喪失からの再照合 | PASS |
| AC-3 | 深さ・ノード・関係・ページ上限、timeout、truncated、Stats欠落、反映待ちを区別し、不完全なManifest保存を拒否 | PASS |
| AC-4 | 複数commitのsnapshot、loading世代の隔離、本文/所属/関係の並行更新、backup復元、実停止/再起動、履歴不足からの再構築 | PASS |
| AC-5 | Workspace・両端・所有者の現在認可、制限付き子、別Userのcursor、所属取消、Manifest保存時の再認可 | PASS |
| AC-6 | 合意データと設定で負荷を独立再実行し、全目標を達成 | PASS |
| INVAR-1〜5 | 外部参照へnative RIDを使わず、PGの固定版と現在権限へ照合。関係を再推論せず、停止時も既存Manifestを変更しない | PASS |

## 独立した再実行

`test:graph`は16.04秒で成功した。`test:graph-load`は185.61秒で成功し、warm p95 121.501 ms、再起動後201.013 ms、最大反映遅延1,320 ms、再構築155,152 ms、コンテナpeak 1,019,691,008 byteだった。DB実サイズ280,454,809 byteはcache 16 MiBを超え、WAL flushは1。測定は先頭ページで行い、ページ継続は機能試験で確認した。

追加の`test:access` 0.44秒、`test:contexts` 1.27秒、`test:search` 13.53秒、`check / test / build`、`git diff --check`、旧search cursor互換試験の`-count=1`も成功した。

## fixture差分の再確認

GitGuardianが、環境変数に続く試験用DB名と、互換試験のprotocol domainを秘密値として検出した。環境変数の読取りを変数へ抽出し、公開固定keyの合成cursorをbinary goldenへ移した後、domain文字列も変数へ抽出した。セキュリティ検査の除外設定は追加していない。

検品者は旧tokenのdecodeと新goldenの192 byteが一致することを確認した。双方のSHA-256は`79C8B84A3F8FD2636D218B97A7FBE2CDD3480E8C078A07FDCD29B348FA35C0B2`。Graph試験2箇所は、変数抽出を戻すと旧commitの全文と一致する。製品コード・条件・測定処理は変わらない。

新表記で実接続`test:graph`を再実行し14.84秒で成功した。最終対象commitのcursor互換試験と差分検査も成功した。

## 判定

総合判定はPASS。blocker、新たな指摘、未判定のACはない。検品担当は追跡ファイルを編集せず、専用project・volumeを後片付けした。CIとマージ後のtree照合は、この固定成果物の検品とは別に確認する。
