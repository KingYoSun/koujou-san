# Issue #27の実装・検証記録

現在の判定は[current-state.md](current-state.md)、有限な範囲は[計画](2026-10-18-issue-27-plan.md)を参照する。基準mainは`4638bbd61510a365a03bb134d2a2415ec521fc45`。本記録は実装途中であり、全条件の達成やリリース完了を示さない。

## 現時点の確認

所有する実Node CLIが見送り問題の合成文をstdoutへ出し、その固定出力をサーバー側HTTPへ渡し、根拠付きの判断を保存して後続RunをQueuedで作成する一巡が成功した。元Runの成功は維持し、モデル要求は一回、usageはHopperへ帰属した。接続先は所有する固定応答HTTPであり、実LLMの意味判断品質や実API契約の成功とは区別する。専用schema・PG・ArcadeDBは試験後に片付けた。`test-results/issue27-semantic-first-success.log`。

既存の決定論HopperのPG試験も成功した。途中で、保存済み評価の再利用が古いleaseを受け入れる回帰を検出し、再利用前にも現在のlease・権限を確認するよう修正した。変更前は`issue27-hopper-baseline-integration.log`、変更後は`issue27-hopper-lease-after.log`。

評価用と発行先Run用のManifestが同じIDになる不備は、評価用の識別子を分けて修正した。最初の発行fixtureではpromptの受渡しbindingも欠けており、こちらはfixtureの設定誤りとして修正した。失敗は`issue27-semantic-mapping-after.log / issue27-semantic-context-separation-after.log`、成功は上記の一巡ログに残した。

サーバー接続の所有HTTP試験で、Workspace・モデルの許可、共有メタデータからのキーと他Workspaceの除外、原HTTPエラーの保持、要求ID、リダイレクト先への資格情報転送拒否が成功した。クライアントとサーバーの応答外形・数量の復号は共通化し、既存clientopsの試験は維持した。

原応答とusageの保存は予約済み観測の完了に限り、予約の照合と過去の保護対象依存の保持を必須にする。要求後の管理者権限失効では発行を拒否し、応答とusageは保持した。権限復旧とlease再取得後は、保存済み応答を追加要求なしで再利用した。`issue27-revocation-replay-after.log`。

## 独立検品と条件別の検証

固定`9783464`の独立検品で、停止・上限確認前の送信、再試行費用の脱落、必須candidateキー欠落の受理を検出した。変更前のunit失敗と四条件の送信は`issue27-review-before.log`、修正後は`issue27-review-cost-rerun.log`。固定`d7fcb5c`の再検品では、再試行待機中に停止した場合も既消費を残す必要があると指摘されたため、意味判断の全終了経路で保存済み試行を集約するよう修正した。

`issue27-semantic-final-local.log`では、元Runの成功、入力分離、停止・深度・回数・期間、再試行費用とWorkspace予算、再試行前と応答後の停止、応答後の失効、判断不能・不成立・形式不正・時間切れ、時間切れ後の回復、遅着証拠の関連評価だけの再実行、定義編集で再評価しないこと、明示再評価の再送収束、送信禁止分類の拒否を確認した。容量超過を入力待ちにして送信しない試験も同fixtureへ追加した。

時間切れの消費は欠測として残す。後の試行が成功しても、前の消費を0にせず全試行と未算定状態を保持する。所有HTTPの固定応答を、実LLMの品質検証とは呼ばない。

全体のunitと共通UIは`issue27-unit-all.log`（UI 18ファイル・64試験）、静的検査は`issue27-check.log`。共通応答処理とusage変更のclientops回帰確認は`issue27-clientops-regression.log`。

## 共通画面・HTTP・MCP

`issue27-ui-fixture-final.log`の実Ory・公式MCP SDK・専用PG/ArcadeDB環境を使い、30 tool、既存のnone/分類/別Workspace境界、HTTP/MCPの固定版・再送、参照操作で処理を増やさないことを確認した。初期fixtureは開発用モデル設定を読み込んでmetadata取得に失敗したため、試験では実接続設定を無効にして所有モデルだけを明示登録した。接続メタデータはContext本文・鍵を含まないので、none操作でもWorkspaceの閲覧権限で読める。

Chromeで接続取得、時間上限の編集・版保存、有効化、固定証拠からの提案、入力と原応答の明示取得、試行表示、提案からのRun発行、再評価と以前の発行先の保持、連鎖停止・再開、過去二件の評価が発行なしで成立するところまで確認した。`issue27-semantic-published.png / issue27-semantic-replay.png`。固定JSONの横へのはみ出しは、Hopper内で折返しと高さ上限を適用した。Windows実機や本人の初見操作は追加依頼していない。

## 任意Gatewayの保存と復旧

[隔離probe](2026-10-18-issue-27-gateway-probe.md)に加え、標準`llm` profileの起動が成功した。管理APIは未認証401、管理ログイン後200。初期providerは未登録なので存在しないモデル要求は400となり、実providerへ要求しない。登録済みproviderのある隔離probeでは、推論APIの未認証401を別に確認している。

停止した専用volumeをtarで保全し、新しい所有volumeへ復元した。`config.db / logs.db`のSHA-256が一致し、同じ設定と暗号鍵で管理ログイン・provider APIが成功した。`issue27-optional-gateway-restore-final.log / issue27-optional-gateway-restore.json`。検証volumeとコンテナは除去した。標準Bifrostは通常停止し、設定とvolumeを保持した。

初回の復元fixtureはroot所有の新volumeへUID1000で展開して失敗した。復旧先だけをrootで展開し、UID/GIDを1000:0へ合わせて修正した。この手順はrunbookにも反映した。

## 残る確認

最終固定commitの独立検品・CI、実providerでの意味判断、マージ後照合が残る。実provider試験は未実行で、接続先・認証と消費枠の本人判断が必要である。未実行の条件を成功や達成とは記録しない。
