# 文書案内

koujou-sanの製品仕様、開発手順、現在の状態をここから参照する。設計上の提案と、実装・検証済みの事実を分けて読む。

Projectの相談は[チャットの保存・仕様採用・関連作業](runbooks/project-chat.md)を参照する。

## 読む順番

1. [共通指示](../AGENTS.md)。
2. [設計書](../koujou-san-design-doc.md)の概要、設計原則、採用技術、今回の作業に関係する節。
3. [開発手順](runbooks/dev.md)と[Issueから実装する手順](runbooks/issue-lifecycle.md)。Context画面は[編集・Scope整理・プレビュー](runbooks/context-ui.md)、GitHub接続は[観測手順](runbooks/github-observation.md)、利用量・単価は[料金と集計](runbooks/usage-pricing.md)、Triage・Workerは[自動化の手順](runbooks/automation.md)、Runは[入力・質問・出力](runbooks/runs.md)、工程は[定義・検証・実行](runbooks/workflows.md)も参照する。
4. 作業と判断は[一覧・回答・回付・差し戻し](runbooks/work-and-decisions.md)、Runの利用量は[モデル・数量・費用](runbooks/run-usage.md)、Hopperは[定義・発行・連鎖](runbooks/hopper.md)、独立検品は[固定入力と証拠の手順](runbooks/inspections.md)、外部起票は[共有要求と作成確認](runbooks/issue-publication.md)、そのほかは関連する[ADR](adr/README.md)、[進捗記録](progress/README.md)、実装とテスト。

UI/UX作業の視覚仕様は[DESIGN.md](../DESIGN.md)を入口に、[基礎](design/foundations.md)、[部品](design/components.md)、[三パターン](design/patterns.md)、[実装対応と確認](design/implementation.md)を読む。企画HTMLと見本は視覚表現の資料であり、製品要件や実装済み機能の根拠にはしない。

固定出力からのContext生成は[生成・採用・自動方針](runbooks/output-context-generation.md)、契約は[ADR 0031](adr/0031-output-context-generation.md)を参照する。

Contextの自動選択とモデル接続は[選択・保存・中断再開](runbooks/client-context-selection.md)、契約は[ADR 0026](adr/0026-client-context-selection.md)を参照する。

サーバーLLMによるHopperの意味判断と任意Bifrostは[サーバーモデル接続](runbooks/server-models.md)、契約は[ADR 0027](adr/0027-server-hopper-semantics.md)を参照する。

設計書第14章の段階A〜Dは開発の順序を示す。環境が起動するだけで、段階Aや初期リリースの受入条件を達成したとは扱わない。

## 記録の置き場所

| 判断すること | 参照先 |
| --- | --- |
| 作業の開始・範囲・終了 | `AGENTS.md` |
| 共通UIの視覚仕様と参照見本 | `DESIGN.md`、`docs/design/` |
| 製品の目的、合意済み方針、提案、未決事項 | `koujou-san-design-doc.md` |
| 確定した設計・構成判断 | `docs/adr/` |
| 実装済みの挙動 | 現行コードと関連テスト |
| セットアップ、起動、検証 | `docs/runbooks/dev.md` |
| Issue・依頼から実装、検品、終了へ進む手順 | `docs/runbooks/issue-lifecycle.md` |
| 計画の粒度と形式 | `PLANS.md`、`docs/templates/implementation-plan.md` |
| 現在の状態、作業結果と証跡 | `docs/progress/` |

IssueとPRは作業の追跡に使う。ユーザーから承認された変更要求は、今回の作業範囲の根拠として扱い、対応する仕様・実装・テストへ反映する。過去のIssueや会話に「完成」とあっても、現在の実装と検証結果を確認する。

同じ条件や結果は一か所で管理し、他の文書からリンクする。古い判断を置き換える場合は、後継文書と失効した範囲を示す。kukuriは構成と開発手順の参考元であり、固有の製品要件や個人の権限をこの製品へ持ち込まない。

## リポジトリの責務

| path | 責務 |
| --- | --- |
| `apps/server/` | Go APIとサーバー側処理 |
| `apps/client/` | WebとTauriで共用するReact UI |
| `apps/client/src-tauri/` | ネイティブ側のOS連携と実行処理 |
| `infra/compose.yaml` | ローカルの標準サービス構成 |
| `scripts/dev.mjs` | rootから使う開発コマンド |
| `.github/` | Issue・PRの雛形とCI |

この配置は開発の入口を用意するもの。Run、Context、認証、検索・関係探索などの製品機能は、Issueや依頼ごとの条件で順に実装する。

標準配布の導入・backup・更新は[配布手順](runbooks/distribution.md)、採用した責務と初期対応環境は[ADR 0029](adr/0029-standard-distribution.md)を参照する。受入検証中の結果は[進捗記録](progress/current-state.md)で確認する。
