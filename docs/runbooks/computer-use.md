# Computer Useによる画面検証

ブラウザとWindowsアプリを、それぞれに用意された操作経路で確認する。UIで設定するパスワードや認証要素の変更、証明書警告への対応はツールの規則に従って本人へ引き継ぐ。通常の画面確認まで一律に本人へ依頼しない。

## 操作経路

Chromeのページは`cua_repl`のChrome拡張経由で開き、DOM・アクセシビリティ・スクリーンショットを使う。ウィンドウやタブのIDを固定値として保存せず、今回の取得結果から選ぶ。空のタブ一覧は切断を意味しない。OAuth等の遷移後に古いframeへの操作やCDP timeoutが発生したら、同じブラウザの現在のタブへ接続し直す。

WindowsのTauri画面はcomputer-useスキルに従い、`node_repl`から`@oai/sky`を使う。`cua_repl`へ同じmoduleをimportしてもskyサービスへ接続できない。現在のウィンドウ一覧から対象を一つに絞り、前面へ出してから状態を取得する。操作後は再取得した状態で結果を確認する。

2026-10-15時点では、Windows操作経路からChromeを取得すると、現在URLを十分な確信度で判定できないという安全上の停止が発生した。これは未解消であり、全体を「Computer Use未構成」とは扱わない。Chrome拡張経由のクリック・入力・送信と、Windowsアプリのボタン操作は成功した。ChromeのDevTools操作を必須にせず、自作アプリの検証は[認証の検証ページ](authentication.md#実ブラウザの通信と保存領域)も使う。

## 接続の診断

まず、今回のブラウザ・タブとWindowsウィンドウを通常APIで列挙する。タイムアウト、切断、権限拒否、URL判定による停止を区別し、同じ失敗を繰り返さない。停止が通知されたターンでは、その停止規則に従う。

Chrome拡張とNative Messaging Hostは、Browserプラグイン同梱の`docs/chrome-troubleshooting.md`にある読み取り専用の診断コマンドで確認する。2026-10-15の環境では、Default profileの拡張がinstalled/enabled、hostのmanifest・registry・拡張originがすべて正常だった。Native Messaging Hostを独自に再登録したり、安全機構を無効化したりしない。

Windowsの操作セッションが古い場合は、`node_repl`のreset後にスキルの初期化と対象選択をやり直す。今回の再確認では、Tauri画面の読み取りと「アカウント設定」ボタンからChromeを開く操作が成功した。

製品側の不具合と操作ツールの不具合を分け、確認できていない結果を成功へ置き換えない。アプリ更新・再起動・再接続が必要な問題は、観測結果と未解消の範囲を残す。

参照: [公式Computer Use手順](https://learn.chatgpt.com/docs/computer-use)、[公式Chrome拡張手順](https://learn.chatgpt.com/docs/chrome-extension)。
