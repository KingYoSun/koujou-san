# koujou-san

CLI、AIエージェント、GitHub、CI、人による判断を接続するソフトウェアファクトリーオーケストラです。製品の要求と設計案は [設計書](koujou-san-design-doc.md) にあります。

現在は実装開始用の開発環境です。Go APIのhealth、Web・Tauriで共有するUI、開発用サービス、検証コマンドとCIを用意しています。Run、Workspace、ログイン、検索・同期などの製品機能はこれから実装します。

## 開発を始める

Node.js 22.x（22.23.4以上）、pnpm 10.16.1、Git、rustupを用意します。Windowsのネイティブ開発にはVisual StudioのC++ Build ToolsとWebView2、サービス起動にはDocker Composeが必要です。Goはセットアップでリポジトリ内へ準備するため、システムのGoを変更しません。

```powershell
pnpm run setup
pnpm run doctor
pnpm run check
pnpm run test
pnpm run build
```

`pnpm run setup` は固定した依存を導入し、開発用秘密値を生成した `.env` を新規作成します。既存の `.env` は保持します。`pnpm setup` はpnpm自身の別コマンドなので、必ず `run` を付けます。

別々のターミナルでAPIとUIを起動します。

```powershell
pnpm run dev:api
pnpm run dev:web
```

APIは `http://127.0.0.1:8080/healthz`、UIは `http://127.0.0.1:1420` です。ネイティブアプリは `pnpm run dev:desktop`、標準サービスは `pnpm run infra:up` で起動します。サービス設定とポートは [infraの案内](infra/README.md) を参照してください。

| 場所 | 内容 |
| --- | --- |
| `apps/server` | Go API。現在は起動確認のみ |
| `apps/client` | 共通React UIと `src-tauri` のネイティブ境界 |
| `infra` | PostgreSQL、ArcadeDB、Oryの開発用構成 |
| `scripts` | セットアップ、環境診断、ローカルとCI共通の検証入口 |
| `docs` | 確定判断、開発・Issue手順、現在状態と検品記録 |
| `.github` | Issue・PR様式とLinux／WindowsのCI |

詳細は [開発手順](docs/runbooks/dev.md)、[文書案内](docs/README.md)、[現在状態](docs/progress/current-state.md) を参照してください。
