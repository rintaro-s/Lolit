# Lolit SolidWorks Add-in

SolidWorks 用 Lolit アドイン。SolidWorks API から部品の質量・体積・BOM・カスタムプロパティを抽出し、Lolit Metadata Server へ送信する。

## 開発要件

- Windows
- Visual Studio 2022 以降 または VS Code + .NET SDK
- SolidWorks 2022 以降
- .NET Framework 4.8

## ビルド

```powershell
cd solidworks-addin
dotnet build -c Release
```

## インストール

1. 管理者権限の PowerShell で `regasm` を実行するか、SolidDNA の Addin Installer を使用。
2. `bin/Release/net48/LolitSolidWorksAddin.dll` を登録。

## 環境変数

- `LOLIT_SERVER` : Lolit Metadata Server URL (default: http://localhost:8080)
- `LOLIT_REPO`   : リポジトリ名 owner/repo (default: team/robot2026)
