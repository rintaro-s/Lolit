# Lolit 仕様書

## 1. 概要

Lolitは、ロボコン開発チーム向けのファイル管理システムである。Git を VCS コアとして採用し、CAD（SolidWorks / KiCAD）に特化した差分表示・検索・ロック機能を追加する。
開発時（学内・自宅等）でのみ使用し、大会会場・合宿先での運用は行わない。


### 1.2.1. 作成するもの

- Lolit Metadata Server
Giteaパーサー。
使いやすいインターフェイスを提供して、Gitを使えない人でも簡単にファイル共有がで１きるようにする。

- Lolit Client
本サービスを使用するためのクライアント。WIndows,Mac,Linuxに対応させる。非常に使いやすくする必要がある。
具体的には、WebUI, Solidworks拡張機能、CLiコマンド、GUIアプリとして提供される必要がある

### 1.2.2. 機能要件
これに合わせて作成して下さい。詳細なことはこれから記載しますが、ここにある機能で具体的な定義がない場合はあなたが要件を決めて。

- Git管理に特化したUI（GitHubに近い体験）。ただしGitを一度も使ったことがない人でも迷わず使える程度にシンプルであること
- WebUIから直接ファイルをドラッグ&ドロップ（またはフォルダ選択）するだけでコミットが作られる、本物の「ドライブ的」簡易アップロード
- 共有されたファイルの履歴保存
- 様々なインターフェイスからの使用
- 安定性
- LAN内のラズパイでホストし、これに接続されているUSBドライブをストレージとする
- ユーザーアカウントと権限管理（誰が何をロックしたか、誰が管理者かが分かる）
- ディレクトリを指定すれば、コマンド/ボタン一発で全てプッシュできるような感じ

これまでは「Google Driveのようなファイル置き場」を軸に設計していたが、それでは「ドライブ的な手軽さ」と
「Gitによる正しい履歴管理」のどちらも中途半端になってしまうと判断し、方針を転換した。
WebUIの情報設計はGitHubのようにリポジトリ・コミット・差分を中心に据える一方、
「Gitを使ったことがない人」のためにドラッグ&ドロップだけでコミットが作れる導線を必ず用意する。
つまりUIの見た目・トンマナ（暗めの配色・角丸・装飾を抑えたデザイン言語）は維持しつつ、
情報設計・機能内容は「ドライブ」ではなく「Git特化のチームファイル共有」に大きく寄せる。

また、いかにズラーッと詳細を並べていますが、仕様は大雑把にしてください。
絶対に、ルールを強制しないで下さい。
詳細に使いたい人には細かく使えて、大雑把に所定のディレクトリをアップロードしてくだけでもいいからそれだけで使えるようにして。
まず「使ってもらうことが」大切です。


### 1.3. 管理対象と方式

| ファイル種別 | 保存方式 | 差分方式 |
|---|---|---|
| C/C++, Python, 設定ファイル等 | Git | 標準テキストdiff |
| KiCAD (.kicad_pcb / .kicad_sch) | Git | S式パースによる部品/Net/BOM単位比較 |
| SLDPRT / SLDASM | Git LFS + LFS Lock | プレビュー画像比較 + メタデータ比較 |
| STEP / STL | Git LFS | プレビュー画像比較 |
| 画像・動画・その他バイナリ | Git LFS | 変更検知のみ |

KiCADファイルの3-wayマージは実施しない。1ファイルにつき同時編集者は1名までとし、Git LFS Lockで強制する。

---

## 2. 技術スタック（確定）

| 項目 | 技術 |
|---|---|
| VCS | Git |
| 大容量ファイル管理 | Git LFS |
| ロック | Git LFS Lock API |
| Git/LFSサーバ実装 | Gitea（セルフホスト） |
| メタデータAPIサーバ | Go |
| CLI | Go（シングルバイナリ配布） |
| SolidWorks連携 | C# Addin |
| KiCAD連携 | Python Plugin |
| WebUI | HTMX + Alpine.js |
| メタデータDB | SQLite |
| 全文検索 | Bleve |
| KiCAD S式パーサ | sexpdata（Python） |
| ストレージ | ZFS（Snapshot機能を利用） |
| 通信 | REST（ファイル/API） + WebSocket（ロック状態通知） |
| 認証 | JWT（Giteaの認証基盤に統合） |

GUIクライアントは提供しない。WebUIをすべてのプラットフォームで共通のGUIとする。

---

## 3. システム構成

```text
Client
 ├── loli CLI (Go)
 ├── WebUI (Browser)
 ├── SolidWorks Addin (C#)
 └── KiCAD Plugin (Python)
        ↓ REST / WebSocket
Gitea (Git + Git LFS + LFS Lock + JWT認証)
        ↓ push webhook
Lolit Metadata Server (Go)
 ├── Metadata DB (SQLite)
 ├── Search Engine (Bleve)
 ├── Preview Generator
 └── Diff Engine (KiCAD / SolidWorks)
        ↓
ZFS Storage (Snapshot)
```

Git本体・LFS・ロック機能はGiteaにすべて委譲する。Lolit Metadata Serverは、Giteaのpush webhookを受けてメタデータ抽出・差分計算・検索インデックス更新・プレビュー生成のみを担当する。

---

## 4. メタデータ抽出パイプライン

### 4.1 起動トリガー

Giteaのpush webhook（`POST /webhook`）をLolit Metadata Serverが受信し、以下を実行する。

```text
push webhook受信
 ↓
変更ファイル一覧取得（git diff --name-status）
 ↓
拡張子ごとに処理を分岐
 ├─ .kicad_pcb / .kicad_sch → KiCAD Diff処理
 ├─ .SLDPRT / .SLDASM       → SolidWorksメタデータ処理
 ├─ .STEP / .STL            → プレビュー生成のみ
 └─ その他                  → 全文検索インデックスのみ更新
 ↓
Metadata DB (SQLite) へ書き込み
 ↓
Bleve インデックス更新
```

### 4.2 KiCAD差分処理

1. `.kicad_pcb` / `.kicad_sch` の新旧バージョンをそれぞれ取得
2. sexpdataでS式をパースし、コンポーネント単位のリスト（リファレンス番号・フットプリント・座標・接続ネット）に変換
3. 新旧リストをリファレンス番号（例: `R1`, `C3`, `U2`）をキーに突き合わせ、以下を検出する
   - 追加された部品
   - 削除された部品
   - フットプリント/定数値が変更された部品
   - ネット接続が変更された部品
4. 結果をJSON形式でMetadata DBに保存し、WebUI上で「部品差分」「Net差分」として表示する

### 4.3 SolidWorksメタデータ処理

SolidWorks APIはサーバ側から呼び出せないため、抽出は必ずクライアント（Addin）側で行う。

1. コミット時、SolidWorks AddinがSolidWorks APIを用いて対象ファイルから以下を抽出する

```json
{
  "file": "robot_arm.SLDASM",
  "commit_hash": "abc123",
  "mass_kg": 2.451,
  "volume_mm3": 981234.5,
  "material": "Aluminum 6061",
  "bom": [
    { "part": "arm_link1.SLDPRT", "qty": 2 },
    { "part": "bolt_M4x10", "qty": 8 }
  ],
  "custom_properties": {
    "Author": "yamada",
    "Revision": "C",
    "Description": "アーム上部リンク"
  }
}
```

2. AddinはこのJSONをLolit Metadata Serverへ`POST /api/metadata`で送信する（push webhookとは独立した経路）
3. サーバ側は同一ファイルの直前コミットのメタデータと比較し、質量・体積・BOM・Propertyの差分を計算してMetadata DBへ保存する
4. プレビュー画像は同じくAddin側で生成（SolidWorks APIのスクリーンショット機能を使用）し、画像ファイルとしてMetadata Serverへアップロードする

### 4.4 プレビュー画像生成（STEP/STL）

サーバ側で以下のいずれかを使用しサムネイルを生成する。

- STEP: OpenCascade（`python-occ`）でレンダリングしPNG出力
- STL: `numpy-stl` + `matplotlib`で簡易レンダリングしPNG出力

生成した画像はZFS Storage上の `/previews/{file_hash}.png` に保存する。

---

## 5. データベーススキーマ（確定）

### 5.1 files テーブル

| カラム | 型 | 内容 |
|---|---|---|
| id | INTEGER PK | |
| path | TEXT | リポジトリ内のパス |
| file_type | TEXT | sldprt / sldasm / kicad_pcb / kicad_sch / step / stl / other |
| latest_commit | TEXT | 最新コミットハッシュ |
| locked_by | TEXT NULL | ロック中ユーザー（未ロックならNULL） |

### 5.2 sw_metadata テーブル

| カラム | 型 | 内容 |
|---|---|---|
| id | INTEGER PK | |
| file_id | INTEGER FK | files.id |
| commit_hash | TEXT | |
| mass_kg | REAL | |
| volume_mm3 | REAL | |
| material | TEXT | |
| bom_json | TEXT | JSON形式のBOM |
| properties_json | TEXT | JSON形式のCustom Property |

### 5.3 kicad_diff テーブル

| カラム | 型 | 内容 |
|---|---|---|
| id | INTEGER PK | |
| file_id | INTEGER FK | files.id |
| commit_hash | TEXT | |
| diff_json | TEXT | 追加/削除/変更部品のJSON |

### 5.4 previews テーブル

| カラム | 型 | 内容 |
|---|---|---|
| id | INTEGER PK | |
| file_id | INTEGER FK | files.id |
| commit_hash | TEXT | |
| image_path | TEXT | ZFS上の画像パス |

### 5.5 users テーブル（Lolit独自アカウント）

| カラム | 型 | 内容 |
|---|---|---|
| id | INTEGER PK | |
| username | TEXT UNIQUE | |
| password_hash | TEXT | bcrypt |
| display_name | TEXT | |
| role | TEXT | admin / member |
| created_at | INTEGER | |

---

## 6. 権限管理（確定）

権限は2層に分かれる。

1. **Git/LFS/Lock/認証そのもの** — 引き続きGiteaの組織・チーム機能にすべて委譲する。学年・担当機体ごとに
   Gitea上のTeamを作成し、リポジトリ単位でアクセス制御する（push/pull/LFS Lockの実際の強制はGiteaが行う）。
2. **Lolit独自のアカウント** — WebUI/APIへのログイン、ロック・アップロードの記名、管理系操作（メンバー管理・
   強制アンロック）の可否を制御する軽量なアカウントで、Giteaのアカウントとは独立している。SQLiteの
   `users` テーブルで管理し、初回登録したアカウントが自動的に管理者になる（以降のアカウント作成は管理者のみ可能）。

| ロール | 権限 |
|---|---|
| admin | 全リポジトリの閲覧 / ロック強制解除 / メンバー管理（追加・削除・権限変更） |
| member | 閲覧 / 検索 / ロック・アンロック / ブラウザからのアップロード |

ブラウザアップロード機能は、Lolit Metadata ServerがGiteaの管理者クレデンシャル（`LOLIT_GITEA_USER` /
`LOLIT_GITEA_PASS`）を使ってGitea側へ実際のpushを代行することで実現する。コミットの author 名/emailは
アップロードを行ったLolitユーザーの名前になるため、Git履歴上の記名は正しく保たれる。

---

## 7. WebUI画面構成（確定）

GitHubのリポジトリページに近い構成を採用する。左サイドバーはGiteaから発見したリポジトリの一覧（切り替え用）、
右メインエリアは選択中リポジトリのタブ切り替えUIとする。

| タブ | 内容 |
|---|---|
| ログイン画面 | Lolitアカウントでのログイン。初回のみ「管理者アカウントの作成」フォームを兼ねる |
| コード | ファイルツリーのブラウズ（フォルダ階層をたどれる）+ ドラッグ&ドロップ/フォルダ選択によるアップロード欄 |
| コミット | リポジトリ全体のコミット履歴 |
| ロック中 | 現在ロック中のファイル一覧、Admin による強制解除 |
| リリース | タグ付けされた機体バージョン一覧、新規作成 |
| 検索 | ファイル名・コミットメッセージ・BOM・Property横断検索（全リポジトリ対象） |
| メンバー（Admin用） | Lolitアカウントの一覧・追加・権限変更・削除 |
| ファイル詳細（サイドパネル） | ロック状態、KiCAD部品差分、SolidWorksメタデータ、そのファイルのコミット履歴 |

Gitを使ったことがない人のために「コード」タブの一番上に常時ドロップゾーンを表示し、
ファイル（またはフォルダ）をドラッグ&ドロップ、またはクリックして選択するだけでコミットが作られるようにする。
Git用語（ブランチ・マージ・ステージング等）はUI上に極力出さない。

見た目のトーン（暗めの配色・角丸・装飾を抑えたデザイン）は既存のCSSを踏襲する。
以下は禁止です。
・グラデーションを多用
・shadcn/uiを初期状態から何も変更せず使用
・中央配置のタイトル
・常時ダークモードの外観
・テンプレート化された特徴グリッド
・ガラス形態
・カードに色つき線
・大事なボタンは紫色
・見出しは全て大文字
・番号付きの手順シーケンス
・タイトルの上にカプセル型のバッジ
・鮮やかなボックスシャドウ
・サイドバーやナビゲーションバーに絵文字を使用

---

## 8. CLIコマンド（確定）

`loli` はGitea上のGit操作をラップするGoバイナリとして提供する。

```bash
loli clone <repo>          # git clone のラッパー
loli commit -m "message"   # git add . && git commit
loli push                  # git push（LFS込み）
loli pull                  # git pull（LFS込み）
loli lock <file>           # git lfs lock <file>
loli unlock <file>         # git lfs unlock <file>
loli locks                 # 現在のロック一覧表示
loli history <file>        # git log --follow <file>
loli search <query>        # Lolit Metadata Server APIへ問い合わせ
loli release <tag>         # git tag + Metadata Serverへリリース登録
loli login [username]      # Lolitアカウントでログイン（トークンをローカルに保存）
loli logout                # 保存済みトークンを破棄
loli whoami                 # ログイン中のアカウントを表示
loli doctor                 # git/git-lfs・Gitea・Lolitサーバへの疎通を診断
```

---

## 9. 簡易アップロード（ブラウザ、確定）

Gitを使えない人向けに、WebUIの「コード」タブからファイル（またはフォルダ）をドラッグ&ドロップ、あるいは
クリックして選択するだけでコミットが作られる経路を用意する。

```text
ブラウザでファイルをドロップ
 ↓
POST /api/upload (multipart/form-data: repo, message, files[], paths[])
 ↓
Lolit Metadata Server が一時ディレクトリへ shallow clone
  (Gitea admin クレデンシャルで認証したURL経由。git-lfs installも実行)
 ↓
アップロードされたファイルを配置 → git add → 呼び出しユーザー名でgit commit
 ↓
git push (Giteaの通常のgit HTTPエンドポイント経由)
 ↓
Gitea が通常のpush webhookを発火 → 4章のメタデータ抽出パイプラインがそのまま動く
```

- アップロード先のパスが既に他ユーザーにロックされている場合は409を返し上書きしない。
- 一時ディレクトリは処理後に削除する（サーバー上にステージング用の永続クローンは持たない）。
- Gitea管理者クレデンシャルが未設定の場合、アップロード機能自体を無効化しエラーメッセージで案内する。

---

## 10. 実装範囲（確定・スコープ外の明記）

### スコープ内
- Gitea連携（Git / LFS / LFS Lock / 認証 / 権限）
- Lolit独自のユーザーアカウント（ログイン・権限・メンバー管理）
- loli CLI（ログイン込み）
- WebUI（本仕様書7章のタブ構成 + ブラウザからの簡易アップロード）
- SolidWorks Addin（コミット/ロック/メタデータ抽出/プレビュー生成）
- KiCAD Plugin（コミット/部品差分/Net差分）
- 全文検索（Bleve）
- ZFSバックアップ

### スコープ外（本仕様書の対象としない）
- 会場・合宿先での運用
- 独立したデスクトップGUIアプリケーション
- Web上での3D CADプレビュー
- Discord/Slack等の外部通知連携
- CI/CD連携
- シミュレーション結果管理