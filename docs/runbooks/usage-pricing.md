# 利用量・料金版・費用評価

Workspaceの「利用量・料金」で、期間集計、export、記録と版履歴、遅着・訂正の範囲を参照する。取得するページは最大100件で、共通UIは50件ずつ表示する。金額・数量は十進数の文字列であり、通貨や評価種別をまたいで合算しない。`records`は重複を除いた計測範囲の数を示し、取得元の原記録の件数とは分ける。

## 料金を設定する

編集者・管理者は「料金の手動設定」で接続先の識別名、契約、実モデル、通貨、適用期間、情報源、丸め規則、課金区分ごとの単価・数量・単位を登録する。中央のモデル一覧への登録は不要。個別契約単価も既定ではWorkspace内で共有する。APIキーや認証情報は入力しない。

既存の料金表は「記録・版履歴」の料金版を参照して編集する。更新は参照した版を前提とし、競合時は現在の版を取得して内容を確認する。通信失敗時に同じ内容を再送しても同じ操作へ収束する。単価の変更だけで過去の費用評価は書き換わらない。

非公開の料金表・案件は共通の資源別権限で保護する。非公開の料金を根拠にした金額も、参照できない利用者の個別取得・集計・exportには含まれない。下位やScopeへの所属は閲覧権限を付与しない。

## APIとMCP

通常認証で次を使用する。

- `POST /usage/workspaces/{workspace}/records/{id}`: 共通mutationと`value`を渡して保存する。利用量のIDは`usage.ObservationID`の安定した生成規則を使う。料金IDは`price:`、費用評価IDは`evaluation:`から始める。
- `GET /usage/workspaces/{workspace}/records/{id}?version={version}`: 固定版を取得する。省略時は現在の版を取得する。
- `POST /usage/workspaces/{workspace}/query`: `mode`に`aggregate / export / records / changes`を指定する。集計・exportではUTCの`from / until`と集計用`timezone`を指定する。終了時刻は含まない。

集計の`kind`は`usage / reported / estimated / api_equivalent / allocated`の一つ。版履歴は`usage / price / evaluation`を選ぶ。帰属種別・帰属先・Workflow・複数Scope・実モデル・通貨で絞り込める。`changes`は`since`の反映位置より後の対象範囲と原記録を返す。次ページには、同じ照会条件と返されたcursorを送る。反映位置と集計時点はページ間で固定され、現在の権限は毎回確認する。サーバーの再起動などでcursorが無効になったら最初から取得する。

原記録は`reported / estimated / missing / not_applicable`とnull・0を区別する。取得元が示す欠測理由は数量の任意の`reason`へ保存し、分からない原因を推測しない。実モデルは不明ならnullにする。累積値は同じ取得元・系列で連番を進める。親総量が子要求を含む場合は`covers_requests`にその要求を指定する。相関が不明な記録は`coverage=unknown`とし、金額を確定済みの合計へ含めない。

費用評価は原記録の現在の計測範囲に対して明示的に作成する。推定額・API料金換算額は固定した料金版を参照する。料金なしでは未算定となる。再評価では新しいIDと前評価への固定参照を指定する。遅着や訂正の後は再評価するまで現在の集計額を再評価待ちとし、旧評価は版履歴から確認できる。換算のレート・時点・情報源は呼出し側が明示する。料金・為替を自動取得しない。

MCPでは`usage_get / usage_query / price_set`を提供する。通常のクライアントが操作を作るとき、手動料金設定には`purpose=price-edit`を明示する。read操作やContext編集操作から料金を変更できない。操作自体の作成や権限の拡張をMCP toolへ公開しない。

## 検証

`pnpm run test:usage`で専用PostgreSQLを起動し、十進数、版履歴、再送、計測範囲、現在認可、通貨別・種別別集計と1万件×3版のページ取得を確認する。`pnpm run test:mcp`は実Oryと公式SDKを使用し、HTTPとMCPの一致を確認する。`pnpm run test:mcp:ui`で共通UIの実操作用環境を起動できる。秘密値を含む`.cache`のfixture設定を公開しない。
