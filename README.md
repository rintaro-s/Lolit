# Lolit

ロボコン開発チーム向けの独自のファイル管理システム。**Gitではない** — バックエンドを自作するのが
手間なので Git + Gitea を実装の都合上流用しているだけで、ユーザーからは「バージョン」「保存」
「ダウンロード」という概念だけが見える。GitHub に近いWebUI・CAD（SolidWorks / KiCAD）差分表示・
検索・ロック機能を提供し、Gitを使ったことがない人でもWebUIにファイルをドラッグ&ドロップするだけで
使い始められる。SolidWorks / KiCAD 側もLolitクライアントとして、ソフト内から完結してバージョン
履歴の閲覧・過去バージョンのダウンロードができる。

> 開発時（学内・自宅等）でのみ使用。大会会場・合宿先での運用は想定していません。

## 構成

| コンポーネント | 技術 | 説明 |
|---|---|---|
| Lolit Metadata Server | Go | Gitea push webhook を受けてメタデータ管理・ログイン認証・簡易アップロードAPIを提供 |
| WebUI | Alpine.js | リポジトリ/コミット/ロック/検索/メンバー管理のタブUI + ドラッグ&ドロップアップロード |
| loli CLI | Go | Git / LFS / Lock のラッパー + ログイン |
| SolidWorks Add-in | C# | メタデータ抽出・プレビュー生成 |
| KiCAD Plugin | Python | 部品 / Net 差分の抽出 |

Git / Git LFS / LFS Lock 自体の認証・権限は Gitea に委譲する。Lolit 独自の軽量アカウント（後述）は
WebUI/API のログインとメンバー管理だけを担当する。

## インストール

### 1. Gitea を用意する

Lolit は Gitea 上のリポジトリを操作する前提。同じ LAN 内で Gitea を動かし、リポジトリと
LFS を有効にしておく（詳細は [docs/setup.md](docs/setup.md)）。

### 2. サーバーをビルド・起動する

```bash
sudo apt install git git-lfs sqlite3
cd lolit-server
go build -o lolit-server .

export LOLIT_DATA_DIR=/var/lib/lolit                       # SQLite/検索インデックス保存先
export LOLIT_REPOS_ROOT=/var/lib/gitea/data/gitea-repositories  # Giteaのbare repoルート
export LOLIT_GITEA_URL=http://localhost:3000
export LOLIT_GITEA_USER=gitea_admin                         # ブラウザアップロード機能に必要
export LOLIT_GITEA_PASS=xxxxxxxx                            # 同上（未設定だとアップロード機能は無効）
export LOLIT_JWT_SECRET=$(openssl rand -hex 32)              # ログインセッションの署名鍵（本番では必須）
export LOLIT_WEBHOOK_SECRET=xxxxxxxx                         # 任意。設定するとwebhookの署名検証を行う

./lolit-server   # デフォルトで :8080 待ち受け
```

Gitea側の各リポジトリ設定 → Webhooks に `http://<server>:8080/webhook`（Push eventsのみ）を追加する。

### 3. 初回アカウントを作る

`http://<server>:8080` を開くと「管理者アカウントの作成」画面が出るので、ユーザー名・パスワードを入力する。
最初に作られたアカウントが自動的に管理者になる。以降のメンバー追加は WebUI の「メンバー」タブから行う。

### 4. CLI をビルドする（任意）

```bash
cd rv
go build -o loli .   # Windowsは -o loli.exe
```

## 使い方

### WebUI

`http://<server>:8080` を開いてログイン。左のリポジトリ一覧から選択し、上部タブで
「コード」（ファイルツリー + ドラッグ&ドロップアップロード）「コミット」「ロック中」「リリース」「検索」
「メンバー」（管理者のみ）を切り替える。Gitを知らなくても、コードタブにファイルをドロップするだけで
自動的に保存・共有される。ファイルをクリックすると開くパネルから、最新版のダウンロードや
（アセンブリなら）関連部品込みのダウンロードもできる。

### loli CLI

```bash
loli login                       # Lolitアカウントでログイン（トークンを保存）
loli clone team/robot2026
cd robot2026
loli commit -m "アームリンクを更新"
loli push
loli lock arm_link1.SLDPRT       # 編集中はロック（他の人は解除まで編集不可）
loli unlock arm_link1.SLDPRT
loli history <file>
loli search <query>
loli release v1.0
loli doctor                      # git/git-lfs・Gitea・Lolitサーバへの疎通とログイン状態を診断
loli whoami / loli logout
```

### SolidWorks Add-in

Windows + Visual Studio で `solidworks-addin/LolitSolidWorksAddin.csproj` をビルドし `regasm` で登録。
SolidWorks内で完結する、独立したLolitクライアントとして動作する（Gitコマンドは一切見えない）。

- **Lolitにログイン** — Lolitアカウントでログイン（トークンは端末に保存される）
- **Lolitに保存** — 現在のリポジトリ全体の変更を1つのバージョンとして保存・共有する。アセンブリを
  保存すると、そのアセンブリツリー全体（サブアセンブリ含む）の部品参照関係も自動的に記録される
- **バージョン履歴** — 現在のファイルの過去バージョン一覧を表示し、そのバージョンだけ、または
  アセンブリなら「関連部品を含めて」フォルダへダウンロードできる（アセンブリの参照が壊れないよう、
  各部品はそのアセンブリを保存した時点のバージョンに正確にピン留めされる）

### KiCAD Plugin

```bash
cd kicad-plugin
pip install -e .
ln -s "$(pwd)/lolit_kicad_plugin" ~/.local/share/kicad/8.0/scripting/plugins/lolit_kicad_plugin
```

`python -m lolit_kicad_plugin.cli post board.kicad_pcb --repo .` で、作業ツリーとHEADの部品差分を
Lolitサーバーへ送信できる（pushを待たずに差分を確認したい場合に使う）。

詳細なセットアップ手順（Raspberry Pi設置、systemd化など）は [docs/setup.md](docs/setup.md) を、
仕様の詳細は [spec.md](spec.md) を参照。

## ディレクトリ

```text
lolit-server/    Go metadata server + WebUI
rv/              Go CLI (loli)
solidworks-addin/ SolidWorks C# add-in
kicad-plugin/    KiCAD Python plugin
docs/            セットアップドキュメント
```

## ライセンス

MIT
