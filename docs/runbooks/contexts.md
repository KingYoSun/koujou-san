# Contextの版・所属・入力確定

採用した構造と初期上限は[ADR 0006](../adr/0006-context-manifest.md)を参照する。認証済みのWebセッションまたはUserのBearerで共通APIへ接続する。Webの変更要求にはBFF CSRFが必要で、Device資格情報から編集しない。新規ContextはWorkspace共有で、既存の制限付きリソースの許可は内容編集で上書きしない。

## API

| 操作 | 入口と内容 |
| --- | --- |
| 取得 | `GET /contexts/workspaces/{workspace}/{kind}/{id}?revision={revision}`。省略時はheadを返し、結果のrefは固定版になる |
| 保存 | `POST /contexts/workspaces/{workspace}/{kind}/{id}`へ`operation_id / expected_version / value`を送る。新規作成の前提版は`0`、更新は現在版。同じ操作の再送は同じ結果へ収束する |
| 解決の確認 | `POST /contexts/workspaces/{workspace}/resolve`へ`run_id / baseline / selection / evidence`を送る。保存せず、ready、missing、Manifest案を返す |

kindは`sentence / paragraph / section / document / scope / membership / baseline / manifest / addition`。四粒度のvalueはkind、本文またはchildrenの固定参照、必要な名前・見出し・添付・由来を持つ。内容の参照にheadを保存することはできない。全型と例はGoの`internal/contexts/model.go`、TSの`context-contract.ts`、`contracts/context-fixtures-v1.json`を参照する。

Scopeのvalueはkind、name、任意のparentで、四階層すべてを作る必要はない。Membershipはscope/contextのIDとpresentを持つ。所属の組合せは同じ安定IDで変更し、解除後も過去のrevisionを取得できる。文書の所属変更は子の所属や本文を変更しない。

Baselineはcontextsとmembershipsの固定参照と、未開始Runの選択inputsを持つ。更新時にprevious、changed、affected_run_idsをサーバーが記録する。Manifest保存のvalueは解決要求と同じ形であり、成功時の選択・構造・実入力と採用操作を保存する。Manifestは上書きせず、別のIDで新しい入力を確定する。

Additionは元Manifest、pause_id、response_idと追加contextを持つ。元の開始時入力へ混ぜない。直接値・output由来の型は後続Run APIとの接続境界であり、ここでは実行やoutput生成を行わない。

## 不足と入力の確認

noneと未指定は無注入。selectはinclude順、型付きの子参照順で組み立て、excludeを別経路にも適用する。最終入力はManifestの`input_utf8`に保持し、その文字列のUTF-8バイト列をSHA-256へ渡せばdigestを照合できる。JSONを再整形してからhashしない。noneは入力を省略し、空バイト列のdigestを持つ。

不足時は`ready=false / missing`を返し、完全なManifestを保存しない。missingには参照切れ、権限不足、外部未取得、取得失敗、容量、候補探索の未完了を示すcodeとref/sourceが入る。保存要求の失敗は共通errorとmeta.completeness.reasonへ理由を返す。必要な参照・不足範囲の確認にはresolveを使う。

外部情報をsnapshotで渡す場合は、固定したURI・版・範囲とcanonical JSONのdigestを照合する。referenceで渡す場合は参照自体を実入力へ含め、本文を取得済みとは表示しない。構造化前の文書をSentenceへ自動登録しない。evidenceはBleve/ArcadeDB担当の取得境界を示すfixtureであり、投影の正本確認や候補取得本体の代用にはしない。

初期値は`KOUJOU_CONTEXT_MAX_BYTES=16777216`、`KOUJOU_CONTEXT_MAX_PLACEMENTS=100000`。サーバー起動時に設定し、不正な値では起動しない。上限を超えた内容を省略・自動要約して確定することはない。

## 検証

`pnpm run test:contexts`は所有する試験専用PGだけを起動・削除し、HTTP経由で四粒度、所属履歴、基準更新、入力不変性、権限取消を照合する。`pnpm run test`はGo生成のContext fixtureをそのままTypeScriptへ渡し、入力構造・原内容・配置・由来・digestを確認する。Context編集GUIは#12、候補取得エンジンは#9/#10、Run実行は#16以降で接続する。
