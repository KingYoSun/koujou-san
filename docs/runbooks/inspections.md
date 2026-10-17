# 独立検品と証拠

Workspaceの「独立検品・証拠」で、検品定義・証拠の一覧をページ取得するか、IDと固定版を指定する。本文は選んだ記録だけ取得する。端末の下書きは自動送信しない。サインアウト・Workspace権限失効では既存の下書き削除方針を使う。

## 定義と実行

1. 「定義・工程」で、検品commandと入力方式を持つ共通Run定義を保存する。検品用のtemplateはContext/Manifestを事前注入せず、promptを受け取れる接続を指定する。
2. 「独立検品・証拠」で定義を作る。`template`はRun定義の固定ID/版、`targets`はURI・commit/revision・digest等の固定成果物、`criteria`はID・条件・必要なら元Contextの固定参照を指定する。
3. 契約検品は`purpose=contract`と`selection.mode=select`、初見検品は`purpose=fresh`と`selection.mode=none`を設定する。Contextは固定参照や既存の選択入力を使い、Workspace全体を自動で渡さない。
4. `required_separation`に必要な次元と水準を指定する。次元は`session/workspace/permissions/external_access/criteria/context`、水準は`declared/observed/enforced`。未確認の能力で必須条件を満たしたとは扱わない。`separation_observations`は明示した機器ログの固定証拠参照で、ログが確認した範囲を残す。
5. 共有へ保存し、取得した固定版を採用する。保存・採用だけでは実行しない。実行許可を持つ人が接続端末IDを指定し、必要なら固定リポジトリID/commitを指定して新しい検品を依頼する。commitは対象成果物の固定版と一致させる。

`kind`は`InspectionRun/HumanReviewRun`。共通Runの入力・Pause・原出力と端末の設定・worktreeは[Run手順](runs.md)を参照する。HumanReviewRunも独自の実行機構を持たず、設定した接続と派生契約を使う。

## 結果と証拠

レポートは`inspection`（固定定義参照）、`manifest_digest`、`verdict`、`criteria_results`、`unverified`を持つ。各条件には`criterion_id/verdict/executed/evidence_refs/unverified`を記録する。判定はPASS/FAIL/INCONCLUSIVE。templateの`derivation.verdict_field`でトップレベルの判定field名を明示できる。指定しない場合は`verdict`。レポートの推奨経路は提案のままで、工程や権限を変更しない。

証拠は別の`inspection-evidence:*`記録に証拠自体の固定URI・revision・digest、対応する固定成果物`targets`と条件ID`criteria`、説明・確認時刻・利用可否を保存する。選んだ条件と全対象を証拠がカバーしない場合はPASSにしない。`kind`は`machine-observation/human-confirmation/ai-comment/implementer-claim`。外部の全量コピーや資格情報は含めない。`state`は`available/unobserved/expired/unavailable`。期限が明確な場合だけ`expires_at`を記録する。所在が失効したら新しい版で状態を更新する。

Run IDから、過去の判定、現在のStale、条件ごとの証拠・未確認、証拠の現在の状態を取得できる。別Runの判定を並べて比較し、対立を合成したPASSにしない。人の例外判断には対象launchと元結果の固定参照、条件ID、`accept-with-exceptions/reject`、理由・未確認を保存する。元の判定や実行許可は変わらない。

対象や条件・必要な入力を変えたら新しい定義を採用し、新しい検品を依頼する。差分検品の`reuse`には元Runの固定参照、再利用する条件ID、変更範囲`changes`、影響確認`impact`、固定証拠`evidence`、`unverified`を残す。必要な根拠が足りなければINCONCLUSIVEのままとする。

## APIと確認

入口は`/inspections/workspaces/{workspace}`。`records/{id}`の固定取得/保存、`adopt`、`start/{run}`、`current/{run}`、`judgments/{id}`、`query`を使う。更新は共通の`operation_id/expected_version`が必要。保存と人の判断には編集、開始には明示的な実行許可が必要である。

`query`の`heads`は種類別メタデータ、`history`は選んだ所有対象の記録を返す。次ページは`next`を`after`へ渡し、履歴では`through`も保持する。取得件数は既存のページ規約と同じ最大100件。通常操作で全Context・全履歴・全原ログを取得しない。

`pnpm run test:inspections`で専用DBの固定案件を確認する。ネイティブの所有する固定CLI受け渡しは、`KOUJOU_TEST_INSPECTION_FIXTURE`に試験用のJSON出力先を指定してDB試験を行い、同じ環境変数で`cargo test fixed_inspection_handoff_keeps_commits_and_inputs_separate -- --ignored --nocapture`を`apps/client/src-tauri`から実行する。出力は合成データのみ。認証情報を渡すファイルではない。
