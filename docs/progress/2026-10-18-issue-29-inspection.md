# Issue #29の独立検品

- 対象: `6189df56f6c54eebe7c5916d6d41889ee7c72626`。基準main `fb336a0df3ac10f4d6b03c7e9ecfc3db6994c399`。Scope revision 1、区分C。
- 担当: 実装者の会話履歴を継承しない別コンテキスト。コード・固定成果物と[計画](2026-10-18-issue-29-plan.md)を照合し、実試験は[検証記録](2026-10-18-issue-29-evidence.md)のログと観測を読み取って判定した。検品担当は変更・外部通信・Docker操作・build・UI入力を行っていない。
- 結果: AC-1〜5・INVAR-1〜3はすべてPASS、blocker 0件。CIとマージ後の照合は後工程とする。

| 条件 | 照合した経路・結果 |
| --- | --- |
| AC-1 | LoadRelease→Init/Render→Up/Bootstrap、二人の登録・確認・BFF認証・招待。Windowsの初回URL→HTTPS/認証config検証→保存→Auth/MCP初期化と、再起動の実画面。既存のWeb/Tauri認証境界は#5の検品・証拠を再利用 |
| AC-2 | 固定image、DB/role分離、TLS/issuer、SMTP、privateな設定、loopbackのAPIと管理proxy、public/admin転送の分離。通常・正規化・encoded pathのHTTP試験 |
| AC-3 | freeze→三DBのstream dump→private archive→再開。restoreは新規先と元ImageIDを要求し、データ復元後にwriterを起動しない。明示起動後の二セッション、固定版・所属・関係・Runと、Bleve/ArcadeDBの再構築・結果・反映位置 |
| AC-4 | migration-onlyの入口とworker開始前の終了。schema 22→23の実migration失敗後に旧Render/三DB/サービスを復旧し、再試行で23へ更新。新版起動後の失敗ではDBを暗黙に巻き戻さずbackupを保持 |
| AC-5 | 自身の原文・DCO方針。Linux依存62件・Windows321件のnotice実体と、MPL対象5件のsource archive収録。NSISのRunner・notice・source同梱と、強制終了しないguard、新規導入・再導入の結果 |
| INVAR-1〜3 | DB正本と再構築、CLI/repo移行と認証SaaS契約が不要な境界、秘密値・backupを公開しない保存/転送/記録経路 |

Linux stage `050ae4df`のrelease.json全9 checksumを独立に再計算し、一致した。対象候補までのserverコード差分は0。Windows stage `6189df56`のLICENSE・NOTICEとroot原文、全noticeと必要なsourceを照合した。画面観測のbinary hashも実体と一致した。NSISのSHA-256は`423807EC1ADE38FA39F93F2A2073CED44AA2181E728F7DDB7EF19C2EB92C5D63`。

検品で初回`tar -C ./bundle`の展開先作成漏れを指摘し、`mkdir -p ./bundle`の追加と、CIで実archiveを展開する変更を確認した。先行検品の内部認証URL・四API prefix不足・MPLソース取得先不足も、修正と再生成後の結果を照合した。

コード・文書の全差分は`git diff --check`でPASS。第三者原文のCRLFと原文末尾空白は検査対象から除いて原文を保全した。これを製品コードの例外や、原文以外の整形免除へ広げない。
