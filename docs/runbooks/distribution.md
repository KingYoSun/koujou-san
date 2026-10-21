# プレビューの導入・backup・更新

未署名WindowsプレビューとLinux x86_64の単一サーバーを対象とする。#29の配布・導入・復旧・更新の検証は完了した。#33のリリース前受入を残しており、公開リリースの完了とは扱わない。採用した責務は[ADR 0029](../adr/0029-standard-distribution.md)、最新判定は[#29の計画](../progress/2026-10-18-issue-29-plan.md)を参照する。

## 共有サーバーの初期化

Linux x86_64、Docker EngineとCompose pluginを用意する。公開HTTPS originへ対応するTLS証明書・鍵、CA bundle、メール配送用SMTP URIが必要になる。アプリimageは配布物から読み込み、PostgreSQL・Ory・ArcadeDBの固定imageを原registryから取得する。利用者側のNode・Go・Rustは不要。

配布archiveを展開し、次のコマンドを使う。例のURL・path・メールを実際の導入先へ置き換える。`smtp-uri`と初期パスワードは本人の端末でprivateなファイルへ保存し、チャットやshellの引数へ秘密値を貼らない。

```sh
umask 077
mkdir -p ./bundle
tar -xzf koujou-san-server-linux-amd64-0.1.0-preview.1.tar.gz -C ./bundle
cd ./bundle
chmod +x ./koujou
./koujou init --bundle "$PWD" --state "$HOME/koujou-state" \
  --url https://koujou.example.com --smtp-file "$HOME/private/smtp-uri" \
  --cert "$HOME/private/fullchain.pem" --key "$HOME/private/key.pem" \
  --ca /etc/ssl/certs/ca-certificates.crt
./koujou up --state "$HOME/koujou-state"
./koujou bootstrap --state "$HOME/koujou-state" \
  --admin-email admin@example.com --password-file "$HOME/private/initial-password"
```

`init`の保存先は新規のdirectoryにする。設定・生成した秘密値・TLS鍵はその保存先で権限を制限して保持する。配布物やGitへ戻さない。公開portは指定originのHTTPS portだけで、DB・Ory管理API・ArcadeDB・Bifrostを公開しない。public originはserverのcontainer内からも到達できる必要がある。

初期管理者もメール確認が必要。Webでログインし、Workspaceを明示的に作成してメンバーを招待する。メンバーの登録・メール確認・招待受諾は別操作である。最初のアクセスへ暗黙に管理者権限を付けない。

Windows版をインストールし、初回画面で同じHTTPS originを入力する。証明書と認証設定の検証成功後に接続先を保存し、既定browserでログインする。接続先ファイルは資格情報を含めず、認証情報は従来どおりWindowsの暗号化保存を使う。未署名プレビューではWindowsが発行元に関する警告を表示する場合がある。

## 状態と停止

```sh
./koujou status --state "$HOME/koujou-state"
./koujou stop --state "$HOME/koujou-state"
```

独立Runnerの開始済みCLIはサーバー停止や画面終了だけでは停止しない。サーバー停止中の報告は既存の再送・照合で扱う。Windowsの手動更新前は新しい実行の受付を止め、現在の実行が終わってRunnerが終了してから画面を閉じる。

## backupと復旧

```sh
./koujou backup --state "$HOME/koujou-state" --backup "$HOME/private/koujou-backup.tgz"
./koujou stop --state "$HOME/koujou-state"
./koujou restore --bundle "$PWD" --state "$HOME/koujou-restored" \
  --backup "$HOME/private/koujou-backup.tgz"
```

backupは書込み側のサービスを停止して3つのDBを取得し、元のサービスを再開する。原文・履歴・認証情報・秘密値を含むため、archiveはprivateな保存先で保持する。自動削除は行わない。対応する元の配布物も保全する。backup時点以降に共有保存されていない端末上の報告まで含むとは扱わない。

restoreは新規の保存先と元の配布物を要求する。同じ公開originを使う場合は、元の導入先を停止してから復旧先を起動する。restoreはデータ復元後にサービスを自動起動しない。端末上の開始済みCLIや外部作用が取り消されたとは扱わず、実状態と権限を確認してから`koujou up --state "$HOME/koujou-restored"`で明示的に再開する。復旧した認証・保存記録と、検索/探索の再構築・反映位置を確認する。確認前に元のDBやbackupを削除しない。

## 更新

```sh
./koujou update --state "$HOME/koujou-state" --bundle "$HOME/releases/new-bundle"
```

配布物のchecksumと版を照合し、書込みを止めて更新前backupを作る。migration失敗時は旧版とデータを復旧し、失敗として終了する。新版の起動後は外部作用を取り消せないため、起動失敗でもDBを自動で戻さず、データとbackupを保持する。修復または元のbundleによる明示restoreを選ぶ。schemaの巻き戻しは通常updateでは受理しない。

## 任意Gatewayと秘密値

Bifrostは初期無効。`bifrost/config.json`は停止後に編集し、`config/connectors/provider.env`へprovider用の秘密値を置ける。接続一覧`config/connectors/server-models.json`は初期空である。使用するprovider・モデル・Workspaceと上限を設定してから、`koujou up --state 保存先 --llm`で任意Gatewayを起動する。管理APIの転送やproviderの認証は[サーバーモデル接続](server-models.md)に従う。

生成したComposeと設定は秘密値を含む。`docker compose config`等の出力を一般ログやIssueへ貼らない。通常の管理CLIは設定や資格情報を表示しない。TLS更新・接続資格情報の変更後は、同じ保存先でサービスを再起動する。

## CA bundleの更新

`init --ca`は指定したbundleを配備先の`config/ca.pem`へコピーする。API・edge・Oryはこのコピーを`SSL_CERT_FILE`で使うため、ホストのCA更新だけでは既存の配備先へ反映されない。任意のBifrostはイメージ内のCAを使い、別途イメージ更新が必要になる。

eMudhra中間CA漏えいへの対応では、修正済みのGo・外部イメージを含む配布物へ更新し、配備先のCAも更新する。[検証サイト](https://emudhra.we-have-a-problem.com/)に各OSの修正済みCA版と12本の検証URLがある。CAの原本を更新し、明示的に追加した社内CA等を維持したbundleを用意する。既存の`config/ca.pem`をホストの標準bundleだけで上書きすると、カスタムCAを失う場合がある。

```sh
./koujou stop --state "$HOME/koujou-state"
cp -p "$HOME/koujou-state/config/ca.pem" "$HOME/private/ca-before-update.pem"
install -m 600 "$HOME/private/updated-ca-bundle.pem" "$HOME/koujou-state/config/ca.pem"
./koujou up --state "$HOME/koujou-state"
```

停止・CAの差し替え・再起動まで実施する。コンテナの実行イメージと`config/ca.pem`のchecksumを記録し、各実行実体から12経路が証明書エラーになることと、通常のHTTPS・モデル接続・認証が成功することを確認する。DNS・timeout・HTTPエラーは証明書拒否の証拠にはしない。検証先へ資格情報や実際の入力を送らず、TLS検証を無効にする設定は使わない。

## buildとOSS表示

開発者はrootで`pnpm run dist:server`、Windowsで`pnpm run dist:desktop`を使う。完全な候補をcommitしてからbuildし、配布物の版・commit・image ID・checksumを記録する。WindowsのRunnerを同梱し、初回プレビューはコード署名しない。

自身のLICENSE・NOTICE、固定依存の`THIRD_PARTY_NOTICES`と原文license、必要な対象ソースを配布物へ含める。第三者imageは原registryと原ライセンスに従って取得する。外部投稿の条件は[CONTRIBUTING.md](../../CONTRIBUTING.md)を参照する。
