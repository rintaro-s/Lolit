# Lolit KiCAD Plugin

KiCAD 用 Lolit プラグイン。`.kicad_pcb` / `.kicad_sch` から部品・ネット情報を抽出し、Lolit Metadata Server へ送信する。

## インストール

```bash
cd kicad-plugin
pip install -e .
```

KiCAD のプラグインフォルダ (`Tools` → `External Plugins` → `Open Plugin Directory`) にシンボリックリンクを貼る:

```bash
ln -s /path/to/kicad-plugin/lolit_kicad_plugin ~/.local/share/kicad/8.0/scripting/plugins/lolit_kicad_plugin
```

## 使い方

- KiCAD PCB Editor で `Tools` → `External Plugins` → `Lolit Commit` を実行
- または CLI から:

```bash
python -m lolit_kicad_plugin.cli extract board.kicad_pcb
python -m lolit_kicad_plugin.cli post board.kicad_pcb --repo /path/to/git/repo
```

`post` は作業ツリーの `board.kicad_pcb` を `--repo` の HEAD コミットと比較し、その差分を
`/api/kicad-diff` に送信する（コミットハッシュも実際の HEAD を使用するので、後続の
push webhook が作る差分と同じコミットに紐づく）。

## 環境変数

- `LOLIT_SERVER` : Metadata Server URL (default: http://localhost:8080)
- `LOLIT_REPO`   : リポジトリ名 owner/repo (default: team/robot2026)
