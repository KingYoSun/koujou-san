# 配布に含める原文表示

自身のコードはrootのApache-2.0 LICENSE・NOTICEに従う。ここでは、依存のpackage metadataだけでは拾えない表示を原文のまま保持する。第三者のライセンスを自身のライセンスへ置き換えない。

`third-party/index.json`に対象と取得元を記録する。WebView2のRustラッパー0.39.1のLICENSEは、そのcrateのVCS情報にあるcommitから取得した。SDK 1.0.3800.47のx64 static loaderは公式NuGetの同ファイルとSHA-256が一致し、SDKのLICENSE・NOTICEを保持した。対象はloaderであり、WebView2 RuntimeやSDK全体を同梱したとは扱わない。

NSIS 3.11は、固定したTauri CLI 2.12.2が使用するarchiveのSHA-1と一致を確認し、そのarchive内のlicenseを保持した。nsis-tauri-utils 0.5.3の原文も含める。SQLite 3.53.2の表示は、固定したlibsqlite3-sysの実際のC sourceから取得した。SQLCipherを同梱したとは扱わない。

通常の依存はbuild時に対象を取得し、原文・noticeと選んだ条件を配布物へ収める。MPL対象のソースをsource archiveへ含める。任意の新しい依存へ古い条件を無条件に適用せず、版や同梱物が変わったら対象を再確認する。
