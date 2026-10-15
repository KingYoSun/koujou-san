# 0010: MCPの操作・送信分類・共通認可

- 状態: Proposed（実接続と独立検品後に確定）
- 日付: 2026-10-16
- 対象: Issue #11、[計画](../progress/2026-10-15-issue-11-plan.md)

## 取得と編集の境界

MCPはGo SDK v1.8.0、Streamable HTTPの2026-07-28を固定して検証する。モデル推論とHostは実装せず、既存のContext・Bleve・ArcadeDB・入力確定を呼ぶ。任意SQL、graph更新、shell、merge、未実装Domainの仮toolを公開しない。

操作はクライアントが現在のWorkspaceと作業から自動作成する。読取/編集の目的と、任意の固定Manifestを保存し、サーバー発行の操作handleをMCP接続へ結び付ける。モデルが渡すtool引数で操作や許可toolを変更しない。Runに紐付く操作ではManifestの固定版とplacementsから範囲を導き、none/excludeを再選択なしで適用する。

既存のUser認可へ操作境界を追加し、Context読取、版照合、検索フィルタ、グラフの両端/所有者、編集transactionで共通に確認する。通常HTTPのPrincipalは操作境界を持たず、既存挙動を維持する。cursorはUser・Workspaceに加えて操作と送信方針の版にも結び付け、別操作へ再利用できないようにする。

## 大量Contextと送信禁止の分類

送信可のContextを逐一列挙しない。既存タグと直接所属Scopeを分類条件に使い、Workspaceの禁止規則を自動適用する。複数条件のどれかに一致すれば送信しない。現在の分類と取得する固定版の分類を確認する。Scope所属は既存の直接所属の意味を保ち、文書構造から新しい所属・閲覧権限を導かない。

Workspaceの方針管理は既存のmanage権限を使い、MCPのtoolには公開しない。通常のMCP編集は現在読める既存Contextを対象とし、送信分類のタグや所属を変更する経路を含めない。Context本文やログにある指示を方針として実行しない。

検索・探索は候補段階で現在の規則を確認し、禁止対象の本文・ID・件数・継続を返さない。本文や構成の取得でも再確認する。固定構成に禁止の子がある場合、元の固定版を書き換えた本文を作らず、その構成の取得を拒否する。許可された小さい文書単位は個別に検索・取得できる。

方針変更だけで全Contextを更新・再索引しない。要求で必要な候補と依存先を、対象ID・版・所属の索引で照合する。操作ごとのメモリやリクエストへWorkspace全体のContext IDを保持しない。大量件数のfixtureで候補数・参照範囲と処理を照合する。

## 認証と記録

同梱クライアントは事前登録し、Oryのaccess tokenを要求ごとにintrospectionする。MCPのresourceとscope、issuer、期限、token use、Identityを検証し、ID Tokenや別resourceのtokenを受理しない。操作handleは認証とは別にUser・Workspace・目的・固定範囲へ結び付ける。

取得・編集の記録は操作ID、利用者、tool/resource、取得したID/版/範囲・固定参照・反映位置・時点に限る。OAuth token、操作handle、本文、対話全体を記録やRun入力へ自動追加しない。

UIの参照先としてAGENTS.mdに挙げられたDESIGN.mdとdocs/adr/0014-uiux-dev-flow.mdは、現時点のリポジトリには存在しない。管理UIを実装する際は既存の共通UIと設計書の責務・フローを確認する。
