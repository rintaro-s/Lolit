# Lolit

ロボコン開発チーム向けファイル共有システム。Git + Gitea を VCS コアとして使い、CAD（SolidWorks / KiCAD）に特化した差分表示・検索・ロック機能を追加する。

> 開発時（学内・自宅等）でのみ使用。大会会場・合宿先での運用は想定していません。

## 構成

| コンポーネント | 技術 | 説明 |
|---|---|---|
| Lolit Metadata Server | Go | Gitea push webhook を受けてメタデータを管理 |
| WebUI | HTMX + Alpine.js | Google Drive 風 UI |
| rv CLI | Go | Git / LFS / Lock のラッパー |
| SolidWorks Add-in | C# | メタデータ抽出・プレビュー生成 |
| KiCAD Plugin | Python | 部品 / Net 差分の抽出 |

## クイックスタート

```bash
# サーバー
sudo apt install git sqlite3
cd lolit-server
go build -o lolit-server .
LOLIT_DATA_DIR=/tmp/lolit-test ./lolit-server

# CLI
cd ../rv
go build -o rv .
./rv version
```

詳細は [docs/setup.md](docs/setup.md) を参照。

## ディレクトリ

```text
lolit-server/    Go metadata server + WebUI
rv/              Go CLI (rv)
solidworks-addin/ SolidWorks C# add-in
kicad-plugin/    KiCAD Python plugin
docs/            セットアップドキュメント
```

## ライセンス

MIT
