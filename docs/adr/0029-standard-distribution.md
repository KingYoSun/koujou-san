# ADR 0029: 標準構成の配布と復旧

- 状態: 採用。実装・受入検証は進行中。
- 本人の判断: Apache-2.0、新規外部投稿へのDCO 1.1、Linux x86_64の単一Composeホスト、接続先設定を持つ未署名Windowsプレビューと手動更新。

## 配布物と入口

共有サーバーは、Go API・Web・HTTPS入口・管理用CLIと、その版に対応するCompose設定生成、Ory・PostgreSQL・ArcadeDB・任意Bifrostの固定版を提供する。アプリのimageと管理CLIを配布物に含め、利用者へNode・Go・Rustのbuildを求めない。第三者のimageは原提供元のregistryから取得する。Oryの開発モード、Mailpit、外部認証SaaS契約を通常導入の前提にしない。検証では所有するメール配送先だけを使う。

公開するのはHTTPS入口だけとする。APIと入口はnetwork namespaceを共有し、API自身と管理用proxyはloopbackでlistenする。Oryのpublic listenerとadmin listenerの転送先を分け、公開listenerからadmin listenerへのrouteを設けない。Kratos/Hydraは独立したDBとroleを持つ。

TLSは導入者が用意した証明書・鍵を使う。初期化時にorigin・鍵との一致・対象hostnameを確認する。CA bundleは導入者の信頼設定から受け取り、アプリimageへ開発用CAや秘密値を焼き込まない。OryのTLS終端設定は、未公開のCompose内接続に適用する。入口は外部からのforwarded headerを採用せず、自身のHTTPS originを転送する。

WindowsはNSISの未署名プレビューとし、独立Runnerを同梱する。初回のサーバーURLをRust側でHTTPS originとして検証し、認証設定を取得・照合できた後に保存する。接続先と資格情報の保存を分け、従来の環境変数による開発接続も保持する。Webの同一origin接続と、画面を閉じた後のRunner継続を変更しない。

## backup・restore・更新

秘密値は新しいprivateな保存先で生成する。管理CLIは設定や秘密値を表示せず、Docker操作を引数列で実行する。Composeのprojectと対象containerのlabelを照合し、別の導入先のDBへ操作しない。

backupは書込み側のサービスを止め、アプリ・Kratos・HydraのDBをストリーム保存する。設定・TLS鍵・認証暗号鍵・接続先の資格情報・任意Gatewayの停止済み保存領域を、権限を制限したarchiveに含める。検索・探索の投影は正本から再構築する。全Context本文を管理CLIのメモリへ列挙しない。

restoreは存在しない保存先に限定し、対応する元の配布物とbackupを照合する。既存の保存先へ暗黙に上書きしない。データの復元とサービスの起動を分け、復元だけで過去の自動処理を起動しない。公開originを保つ切替では元のサービスを停止してから復旧先を起動する。

更新は書込みを止めた状態でbackupし、新版のmigrationだけを先に実行する。migration失敗時は旧版とDBを復旧する。新版のアプリを起動した後は外部作用が生じ得るため、その後の起動失敗を理由にDBを自動で巻き戻さない。データと更新前backupを保持して修復・明示restoreへつなげる。更新先・失敗fixtureの具体的な判定は[#29の計画](../progress/2026-10-18-issue-29-plan.md)に従う。

## ライセンスと外部投稿

自身のコードはApache-2.0とし、原文LICENSEとNOTICEを含める。第三者のライセンスは変更しない。配布対象の固定した依存・原文・noticeを収集し、MPL対象のソースはsource archiveへ含める。判定できない条件を適合済みとして配布しない。第三者imageの取得元・版・表示と、実際に再配布するアプリの依存を区別する。

新しい外部投稿にはDCO 1.1の本人のsign-offを求める。既存履歴の改変や、AIによる本人の法的な表明の代行は行わない。DCO文書は原文を維持する。

設定は固定版の[Ory Hydra schema](https://github.com/ory/hydra/blob/v26.2.0/.schema/config.schema.json)と[TLS終端schema](https://github.com/ory/hydra/blob/v26.2.0/oryx/configx/tls.schema.json)、[TauriのWindows installer](https://v2.tauri.app/distribute/windows-installer/)、[sidecar](https://v2.tauri.app/develop/sidecar/)を確認した。文書の存在と、配布物での検証成功は区別し、結果は進捗記録に残す。
