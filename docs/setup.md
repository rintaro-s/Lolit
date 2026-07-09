# Lolit セットアップガイド

Lolit はラズパイ上で動作する **Lolit Metadata Server** と、各クライアントから構成されるファイル共有システムです。

## 1. システム構成

```text
Client (Windows/Mac/Linux)
 ├── rv CLI (Go)
 ├── WebUI (Browser)
 ├── SolidWorks Add-in (C#)
 └── KiCAD Plugin (Python)
        ↓ REST / WebSocket
Lolit Metadata Server (Go)  ← Gitea push webhook
        ↓
SQLite + Bleve + ZFS/USB storage
```

Git / Git LFS / LFS Lock / 認証は Gitea に委譲します。

## 2. Raspberry Pi へのサーバー設置

### 2.1 前提

- Raspberry Pi 4/5 推奨
- Raspberry Pi OS (64bit) または Ubuntu Server
- USB ドライブを `/mnt/lolit-storage` などにマウント済み
- Gitea を同じ LAN 内で動作させる（推奨）

### 2.2 Gitea のインストール（推奨）

Gitea 公式ドキュメントに従いインストール:

```bash
sudo apt update
sudo apt install -y git sqlite3
# Gitea バイナリを /usr/local/bin/gitea に配置
gitea --version
```

`app.ini` の例:

```ini
[server]
DOMAIN = raspberrypi.local
HTTP_PORT = 3000
ROOT_URL = http://raspberrypi.local:3000/

[database]
DB_TYPE = sqlite3
PATH = /var/lib/gitea/data/gitea.db

[repository]
ROOT = /var/lib/gitea/data/gitea-repositories

[lfs]
PATH = /mnt/lolit-storage/lfs
```

### 2.3 Lolit Metadata Server のビルド

ラズパイ上でビルドするか、クロスコンパイル済みバイナリを配置します。

```bash
cd /opt/lolit/lolit-server
go build -o lolit-server .
```

環境変数:

| 変数 | 説明 | デフォルト |
|---|---|---|
| `LOLIT_LISTEN` | サーバー待受アドレス | `:8080` |
| `LOLIT_DATA_DIR` | SQLite / インデックス保存先 | `/var/lib/lolit` |
| `LOLIT_REPOS_ROOT` | Gitea の bare repo ルート | `/var/lib/gitea/data/gitea-repositories` |
| `LOLIT_GITEA_URL` | Gitea URL | `http://localhost:3000` |
| `LOLIT_GITEA_USER` / `LOLIT_GITEA_PASS` | Gitea 管理者アカウント。WebUI からのブラウザアップロードが、このアカウントで代理pushする | 未設定（アップロード機能は無効化） |
| `LOLIT_WEBHOOK_SECRET` | Webhook 署名検証用シークレット（後述） | 未設定（検証なし） |
| `LOLIT_JWT_SECRET` | WebUI/API のログインセッション署名鍵 | 未設定（安全でないデフォルト値。本番では必ず設定） |

起動:

```bash
export LOLIT_DATA_DIR=/mnt/lolit-storage/lolit
export LOLIT_REPOS_ROOT=/var/lib/gitea/data/gitea-repositories
export LOLIT_GITEA_USER=gitea_admin
export LOLIT_GITEA_PASS=xxxxxxxx
export LOLIT_JWT_SECRET=$(openssl rand -hex 32)
./lolit-server
```

`LOLIT_JWT_SECRET` を設定しないと、デフォルトの安全でない値でログインセッションが署名されます。
ラズパイ以外からアクセスできる環境では必ず固有の値を設定してください。

### 2.4 Webhook の設定

Gitea のリポジトリ設定 → Webhooks → `http://raspberrypi.local:8080/webhook` を追加。
トリガーは **Push events** のみで OK。

`/webhook` は誰でも POST できてしまうため、`LOLIT_WEBHOOK_SECRET` を設定して署名検証を有効にすることを推奨します。
Gitea 側の Webhook 設定画面の「Secret」に同じ値を入力すると、Gitea が `X-Gitea-Signature` ヘッダ（HMAC-SHA256）を付与するようになり、
サーバーはそれを検証してから push を処理します。未設定の場合は検証なしで動作します（開発用途向け）。

### 2.5 systemd サービス化（推奨）

`/etc/systemd/system/lolit-server.service`:

```ini
[Unit]
Description=Lolit Metadata Server
After=network.target

[Service]
Type=simple
User=git
Environment="LOLIT_DATA_DIR=/mnt/lolit-storage/lolit"
Environment="LOLIT_REPOS_ROOT=/var/lib/gitea/data/gitea-repositories"
ExecStart=/opt/lolit/lolit-server/lolit-server
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now lolit-server
```

### 2.6 最初のアカウント作成

Lolit の WebUI/API はGiteaとは別に、独自の軽量なアカウント（ログイン・権限管理用）を持ちます。
サーバー起動直後はアカウントが1つも存在しないため、最初にアクセスした人がそのまま管理者になります。

`http://raspberrypi.local:8080` を開くと「管理者アカウントの作成」画面が表示されるので、
ユーザー名・パスワードを入力して作成してください。以降のメンバー追加は、WebUIの「メンバー」タブ
（管理者のみ表示）から行います。

## 3. クライアントのセットアップ

### 3.1 rv CLI

```bash
cd rv
go build -o rv .
# Windows の場合: go build -o rv.exe .
```

`rv` を PATH の通った場所に配置。まず Lolit アカウントにログインします（WebUIで作成したユーザー名/パスワード）。

```bash
rv login
```

```bash
rv clone team/robot2026
cd robot2026
# ファイルを編集
rv commit -m "アームリンクを更新"
rv push
rv lock arm_link1.SLDPRT
```

セットアップ後の動作確認には `rv doctor` が便利です。git / git-lfs のインストール状況、
Gitea・Lolit メタデータサーバへの疎通、ログイン状態を一括でチェックします。

```bash
rv doctor
```

### 3.2 WebUI

ブラウザで `http://raspberrypi.local:8080` を開き、Lolit アカウントでログインする
（初回のみ管理者アカウントの作成画面が出ます）。左のリポジトリ一覧はGiteaから自動的に検出されるため、
リポジトリ名を手入力する必要はありません。

Git を使ったことがない人は、ログイン後に「コード」タブを開き、ファイル（またはフォルダ）を
ドラッグ&ドロップするだけでアップロードできます。裏側では自動的にコミットが作られ、push されます。

### 3.3 SolidWorks Add-in

Windows + Visual Studio で `solidworks-addin/LolitSolidWorksAddin.csproj` をビルドし、`regasm` で登録。
SolidWorks 起動後、Lolit ツールバーからコミット実行。

### 3.4 KiCAD Plugin

```bash
cd kicad-plugin
pip install -e .
```

KiCAD のプラグインディレクトリにシンボリックリンクを貼る:

```bash
ln -s /path/to/kicad-plugin/lolit_kicad_plugin \
  ~/.local/share/kicad/8.0/scripting/plugins/lolit_kicad_plugin
```

## 4. 注意事項

- ZFS の利用は推奨設定ですが、USB ドライブが ext4 でも動作します。
- LFS Lock は Gitea の権限に依存します。Admin / Maintainer のみが解除可能です。
- 大会会場・合宿先での運用は想定していません。
