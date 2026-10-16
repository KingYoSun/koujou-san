# 文書案内

koujou-sanの製品仕様、開発手順、現在の状態をここから参照する。設計上の提案と、実装・検証済みの事実を分けて読む。

## 読む順番

1. [共通指示](../AGENTS.md)。
2. [設計書](../koujou-san-design-doc.md)の概要、設計原則、採用技術、今回の作業に関係する節。
3. [開発手順](runbooks/dev.md)と[Issueから実装する手順](runbooks/issue-lifecycle.md)。Context画面は[編集・Scope整理・プレビュー](runbooks/context-ui.md)、GitHub接続は[観測手順](runbooks/github-observation.md)、利用量・単価は[料金と集計](runbooks/usage-pricing.md)、Triage・Workerは[自動化の手順](runbooks/automation.md)、Runは[入力・質問・出力](runbooks/runs.md)も参照する。
4. 関連する[ADR](adr/README.md)、[進捗記録](progress/README.md)、実装とテスト。

設計書第14章の段階A〜Dは開発の順序を示す。環境が起動するだけで、段階Aや初期リリースの受入条件を達成したとは扱わない。

## 記録の置き場所

| 判断すること | 参照先 |
| --- | --- |
| 作業の開始・範囲・終了 | `AGENTS.md` |
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
