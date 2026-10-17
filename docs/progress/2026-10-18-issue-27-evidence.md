# Issue #27の実装・検証記録

現在の判定は[current-state.md](current-state.md)、有限な範囲は[計画](2026-10-18-issue-27-plan.md)を参照する。基準mainは`4638bbd61510a365a03bb134d2a2415ec521fc45`。本記録は実装途中であり、全条件の達成やリリース完了を示さない。

## 現時点の確認

所有する実Node CLIが見送り問題の合成文をstdoutへ出し、その固定出力をサーバー側HTTPへ渡し、根拠付きの判断を保存して後続RunをQueuedで作成する一巡が成功した。元Runの成功は維持し、モデル要求は一回、usageはHopperへ帰属した。接続先は所有する固定応答HTTPであり、実LLMの意味判断品質や実API契約の成功とは区別する。専用schema・PG・ArcadeDBは試験後に片付けた。`test-results/issue27-semantic-first-success.log`。

既存の決定論HopperのPG試験も成功した。途中で、保存済み評価の再利用が古いleaseを受け入れる回帰を検出し、再利用前にも現在のlease・権限を確認するよう修正した。変更前は`issue27-hopper-baseline-integration.log`、変更後は`issue27-hopper-lease-after.log`。

評価用と発行先Run用のManifestが同じIDになる不備は、評価用の識別子を分けて修正した。最初の発行fixtureではpromptの受渡しbindingも欠けており、こちらはfixtureの設定誤りとして修正した。失敗は`issue27-semantic-mapping-after.log / issue27-semantic-context-separation-after.log`、成功は上記の一巡ログに残した。

サーバー接続の所有HTTP試験で、Workspace・モデルの許可、共有メタデータからのキーと他Workspaceの除外、原HTTPエラーの保持、要求ID、リダイレクト先への資格情報転送拒否が成功した。クライアントとサーバーの応答外形・数量の復号は共通化し、既存clientopsの試験は維持した。

原応答とusageを保存する予約は、発行の現在権限と分けている。既存の所有Domain向けusage保存へ、予約済み観測を完了する経路を追加した。予約の照合を必須にし、過去の保護対象依存を残す。権限失効やWorker停止の各境界、料金と連鎖の集計、再送については、引き続き専用PG試験で確認する。

## 残る確認

遅着証拠・訂正の索引付き再評価、停止/再送/失効/容量/障害/費用のfixture、GUI/HTTP/MCPの照合、共通画面、Bifrost任意サービスの起動・保存・更新復旧と配布通知、必要な独立検品・最終CI・マージ後照合が残る。実providerへの接続は実行していない。必要な段階で、具体的な接続先と合成入力・消費上限を本人へ確認する。
