# MCP接続と送信方針

MCPは既存のContext・検索・関係探索を公開する。モデル推論とMCP Hostは後続Issueの対象であり、この接続だけでLLMやCLIは動かない。設計は[ADR 0010](../adr/0010-mcp-boundaries.md)、条件と検証結果は[Issue #11の記録](../progress/2026-10-15-issue-11-plan.md)を参照する。

## 初期化と認証

通常の開発環境を起動し、同梱クライアントの登録を更新する。

```powershell
pnpm run auth:bootstrap -- --clients-only
```

既存Identityのパスワードは変更しない。共有UIの`/mcp`と`/.well-known/oauth-protected-resource/mcp`はGo APIへproxyする。本番の配置でも同じ公開URLへ転送する。

| 項目 | 値 |
| --- | --- |
| SDK | `github.com/modelcontextprotocol/go-sdk v1.8.0` |
| プロトコル | `2026-07-28`、Streamable HTTP、stateless |
| resource | 公開Site URL + `/mcp` |
| scope | `koujou.mcp` |
| 登録 | 既存のWeb・Tauriクライアントをセットアップ時に事前登録 |
| 認証 | Oryのcode + PKCE。要求ごとにaccess tokenと現在のIdentityを確認 |

保護リソースのmetadataからOryの発見情報へ進む。認可要求とtoken交換へ同じ`resource`を指定する。Ory brokerは元の認可要求のresourceをMCPのaudienceへ対応付ける。通常APIのtoken、別audience・scope、期限切れ、ID TokenはMCPの認証に使えない。メール確認、利用停止、TOTP利用時の認証強度は通常APIと共通に検証する。

## 操作の作成

通常の認証済みAPIから`POST /mcp/control/workspaces/{workspace}/operations/{id}`へ、`operation_id`、`expected_version: "0"`、`purpose: "read"`または`"edit"`を送る。クライアントが作業開始時に自動作成するための入口であり、Contextを逐一選択する操作ではない。

Runの固定入力を参照する場合は、`purpose: "read"`と固定Manifestの`manifest: {kind,id,revision}`を渡す。サーバーがplacementsから取得可能なID・版を導く。noneは空の範囲になり、除外したContextや別の版を取得できない。

返されたhandleを`X-Koujou-Operation`、Workspaceを`X-Koujou-Workspace`、MCP用のaccess tokenを`Authorization: Bearer …`に設定して`/mcp`へ接続する。handleを本文、対話、Run入力、ログへ保存しない。操作はUserとWorkspaceに結び付き、モデルのtool引数から変更できない。終了時は通常APIの`POST …/operations/{id}/close`で失効する。

## 公開する操作

| tool/resource | 結果と制約 |
| --- | --- |
| `context_search` | Bleveの候補ID・固定版・件数・継続・位置・完全性。本文は含まない |
| `context_graph` | ArcadeDBの許可された関係候補。本文は含まない |
| `context_get` | 固定版のContext本文・構成。禁止の子を含む構成は取得を拒否 |
| `context_edit` | 既存Contextを通常APIと同じCAS・操作IDで編集。分類タグや所属は変更不可 |
| `context_resolve` | 固定参照による入力プレビュー。任意の検索・探索を指定した場合はサーバーで再実行し、不完全な候補から完全なManifestを作らない |
| `koujou-context:///{workspace}/{operation}/{kind}/{id}/{revision}` | `context_get`と同じ権限・固定版によるresource取得 |

候補の継続はUser・Workspace・操作・送信方針の版・検索条件・投影位置に結び付く。別の操作へ持ち越さない。resource URIもWorkspace・操作で分け、本文のresource応答はprivate・TTL 0、HTTP応答はno-storeにする。検索・探索が遅延・打切り・必要版未反映なら、その状態を表示する。プレビューはManifestの保存やRun開始を行わない。

新しいDomainを追加するときは、そのDomainの共通業務処理をtoolへ接続する。操作Principal、現在の認可、CAS・操作ID、完全性、操作履歴を引き継ぐ。model通信・shell・任意SQL・graph更新をこの経路へ追加しない。未実装Domainの仮toolは置かない。

## 送信禁止の分類と履歴

Workspace管理者は共通UIの「LLMへの送信方針」で禁止タグと直接所属Scopeを設定する。いずれかに一致するContextを検索・取得から自動で除外する。規則変更のために全Contextを更新・再索引せず、通常操作に全Contextの許可一覧を持たせない。

通常APIの`GET/POST …/policy`でも同じ方針を取得・更新できる。更新にはmanage権限、元版、操作IDが必要。MCPには方針を変えるtoolを公開しない。方針が空でも閲覧権限と操作の範囲は適用される。

`GET …/operations/{id}/events`は操作者本人が取得する履歴で、1ページ100件。`next`を次の`after`へ渡す。閉じた操作の履歴も、現在のWorkspace権限があれば確認できる。User・操作、取得したID/版、返却範囲、反映位置、時刻、編集の操作IDを記録する。固定Context版から、その版が保持する添付・外部参照を照合できる。本文、OAuth token、handle、対話全体は記録しない。

## 検証

```powershell
pnpm run test:mcp
pnpm run test:mcp:ui
```

両方とも専用のPostgreSQL・Ory・ArcadeDBと実Bleveを使い、標準環境のvolumeを変更しない。自動試験は公式SDK、実認証、競合・再送、none/exclude、現在の権限、部分結果、1万Context×3版での禁止分類を確認する。

`test:mcp:ui`は共通UIをbuildし、同じ試験用Userで実APIを呼ぶloopback限定の検証画面を表示する。ログに出たURLで操作する。ブラウザへ資格情報を渡さず、試験用gatewayがそのUserの通常API tokenを設定する。製品の認証経路には含まれない。確認後は、そのURLの`/__fixture/finish`へPOSTして終了する。5分以内に終了しなければ試験を失敗とし、専用環境を片付ける。
