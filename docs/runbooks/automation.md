# Triage方針・優先度・処理状態

Workspaceの「Triage方針・優先度・処理状態」で、方針の版、優先度、処理要求、履歴を確認する。ページは50件ずつ取得する。決定論Hopperの定義・発行は[Hopperの手順](hopper.md)を参照する。LLM評価は後続の実装で接続する。

## 方針と優先度

編集者・管理者は「初期テンプレートで新しい方針を作る」からP0〜P3を読み込める。名称・段階数・順序・基準は変更可能。対象Scopeと方針の優先順を設定する。一つのIssueへ適用する方針は独立して選び、同順位で競合する場合と未設定の場合は評価待ちにする。Scopeへの所属で閲覧権限を付与したり、複数方針を暗黙に合成したりしない。

方針の保存は版を追加する。管理者は保存した固定版を有効化でき、自動反映の有無を指定する。新しい版を保存しても有効版は自動で切り替わらない。有効化だけで過去の全Issueへ要求を追加しない。過去への適用は一ページずつ明示し、返された継続位置から対象を確認して続ける。

Issueの優先度を取得し、理由と優先度を指定すると手動固定になる。固定中も新しい推奨は記録されるが、採用値は維持する。固定解除は現在の対象版・有効な方針に対応する推奨だけを反映する。未評価・失敗・情報不足・再評価待ちは優先度の値とは別の状態であり、既存の値を最低優先度へ変更しない。

## 管理者の設定

Hopper/Triageごとに設定を読み込み、有効状態、対象取得、結果確定、同時処理枠、試行上限、担当・処理期限、再試行間隔、期間内の試行上限を保存する。保存した管理者の現在のWorkspace権限を確認する。ログアウトだけでは無効にならず、管理者の離脱・役割変更・対象権限の失効・管理設定の停止では取得や確定を保留する。クライアントのCLI認証や個人キーをコピーしない。

費用上限を使う場合は、上限・通貨・評価種別を指定する。#14の固定費用評価を使い、異なる種別や通貨を合算しない。利用量が欠測・参照不能なら保留し、0として続行しない。実際の利用量の遅延がある場合、件数上限を併用する。

停止は未確定の処理に適用し、確定済み副作用を取り消さない。設定・権限を復旧した後は、履歴を確認して明示的に再開する。再開時も現在の対象版・方針・権限を確認する。根拠が変わった古い要求は再利用せず、現在の対象へ明示適用する。

## APIとMCP

- `GET /automation/workspaces/{workspace}/template`: 初期の編集可能な優先度尺度。
- `GET/POST /automation/workspaces/{workspace}/records/{id}`: 固定版の取得、期待版と操作IDを持つ保存。IDの種類は`policy / activation / control / priority`。
- `POST /automation/workspaces/{workspace}/query`: `heads / records / jobs / history / priority / selection`。個別・一覧とも現在の権限を確認し、根拠が非公開なら結果へ含めない。
- `POST /automation/workspaces/{workspace}/apply-past`: 固定方針、継続位置、最大100件の明示適用。結果に要求数と次の位置を返す。
- `POST /automation/workspaces/{workspace}/jobs/{id}`: 管理者による停止・明示再開。共通CASとreceiptを使う。

MCPは`triage_get / triage_query / triage_set / triage_apply_past / triage_job_control`を提供する。通常のクライアントから編集目的または管理目的を先に作り、現在の役割と資源別権限の範囲で操作する。read/Context editだけでは編集できない。Workerのlease値・checkpoint本文は公開しない。

## 開発・検証

`pnpm run test:automation`は専用PGで受理・要求の原子性、再送、並行取得、停止・期限・回収、枠・予算、手動固定を確認する。`pnpm run test:mcp`は実Oryと公式SDKでHTTP/MCPの共通経路を確認する。`pnpm run test:mcp:ui`はWeb/Tauri実操作用の専用環境を起動する。

結果を所有するDomainは、実際に共有受理するtransactionから`AcceptResultTx`を呼ぶ。端末の完了観測や未取得ログを受理の代わりにしない。後続Domainの実行器は`RunOne`へ接続し、外部I/Oはtransactionを保持せず行う。結果と再開位置はDBへ保存し、再取得後に外部呼出しを無条件に繰り返さない。
