using System;
using System.Collections.Generic;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Runtime.InteropServices;
using System.Text.Json.Serialization;
using System.Windows.Forms;
using CADBooster.SolidDna;

namespace LolitSolidWorksAddin
{
    /// <summary>
    /// Lolit SolidWorks Add-in: lets people manage file history and sharing
    /// entirely from inside SolidWorks, without ever touching Git directly.
    /// Lolit is its own file-management system; it happens to keep its data
    /// in a Gitea-backed repository as an implementation detail, so none of
    /// that should leak into the UI -- users see "versions" and "save", not
    /// commits/branches/push.
    /// </summary>
    [Guid("A1B2C3D4-E5F6-7890-1234-567890ABCDEF")]
    [ComVisible(true)]
    public class LolitAddIn : SolidAddIn
    {
        private const string ServerEnv = "LOLIT_SERVER";
        private LolitClient? _client;

        public override void PreConnectToSolidWorks()
        {
            SolidWorksAddInTitle = "Lolit";
            SolidWorksAddInDescription = "Lolit file sharing integration for SolidWorks";
        }

        public override void PreLoadPlugIns()
        {
        }

        public override void ApplicationStartup()
        {
            var items = new List<CommandManagerItem>
            {
                new CommandManagerItem
                {
                    Name = "Lolitにログイン",
                    Tooltip = "Lolitアカウントにログイン",
                    Hint = "Lolitアカウントにログイン",
                    ImageIndex = 0,
                    VisibleForParts = true,
                    VisibleForAssemblies = true,
                    VisibleForDrawings = true,
                    OnClick = OnLoginClicked,
                },
                new CommandManagerItem
                {
                    Name = "Lolitに保存",
                    Tooltip = "現在のファイルの新しいバージョンをLolitに保存",
                    Hint = "現在のファイルの新しいバージョンをLolitに保存",
                    ImageIndex = 0,
                    VisibleForParts = true,
                    VisibleForAssemblies = true,
                    VisibleForDrawings = false,
                    OnClick = OnSaveClicked,
                },
                new CommandManagerItem
                {
                    Name = "バージョン履歴",
                    Tooltip = "過去のバージョンを見る・ダウンロードする",
                    Hint = "過去のバージョンを見る・ダウンロードする",
                    ImageIndex = 0,
                    VisibleForParts = true,
                    VisibleForAssemblies = true,
                    VisibleForDrawings = false,
                    OnClick = OnHistoryClicked,
                },
            };
            CommandManager.CreateCommandMenu(title: "Lolit", id: 150_100, commandManagerItems: items);
        }

        // ---------- login ----------

        private LolitClient? EnsureClient()
        {
            if (_client != null) return _client;

            var server = Environment.GetEnvironmentVariable(ServerEnv) ?? "http://localhost:8080";
            var saved = LolitCredentials.Load();
            if (saved != null && saved.Server == server && !string.IsNullOrEmpty(saved.Token))
            {
                _client = new LolitClient(server, saved.Token);
                return _client;
            }

            MessageBox.Show("Lolitにログインしてください。", "Lolit");
            return LoginFlow(server);
        }

        private LolitClient? LoginFlow(string server)
        {
            using var dlg = new LoginDialog();
            while (true)
            {
                if (dlg.ShowDialog() != DialogResult.OK) return null;
                var client = new LolitClient(server, "");
                try
                {
                    client.Login(dlg.Username, dlg.Password);
                    new LolitCredentials { Server = server, Token = client.Token }.Save();
                    _client = client;
                    return client;
                }
                catch (Exception ex)
                {
                    dlg.ShowError("ログインに失敗しました: " + ex.Message);
                }
            }
        }

        private void OnLoginClicked()
        {
            var server = Environment.GetEnvironmentVariable(ServerEnv) ?? "http://localhost:8080";
            if (LoginFlow(server) != null)
                MessageBox.Show("ログインしました。", "Lolit");
        }

        // ---------- save (= git commit + push, hidden behind "save a version") ----------

        private void OnSaveClicked()
        {
            var model = SolidWorksEnvironment.Application.ActiveModel;
            if (model == null)
            {
                MessageBox.Show("開いているファイルがありません。", "Lolit");
                return;
            }

            var repoRoot = GitHelper.FindRepoRoot(model.FilePath);
            if (repoRoot == null)
            {
                MessageBox.Show("このファイルはLolitで管理されているフォルダの中にありません。", "Lolit");
                return;
            }
            var repoName = GitHelper.RepoNameFromOrigin(repoRoot);
            if (repoName == null)
            {
                MessageBox.Show("Lolitのリポジトリ情報を取得できませんでした。フォルダの取得(clone)からやり直してください。", "Lolit");
                return;
            }
            var client = EnsureClient();
            if (client == null) return;

            string note;
            using (var dlg = new TextInputDialog("Lolitに保存", "このバージョンのメモ（任意）", "例: アームリンクの厚みを変更"))
            {
                note = dlg.ShowDialog() == DialogResult.OK ? dlg.Value : "";
            }
            if (string.IsNullOrEmpty(note)) note = "SolidWorksから保存";

            GitHelper.RunGit(repoRoot, "add -A");
            var committed = GitHelper.RunGit(repoRoot, $"commit -m \"{note.Replace("\"", "'")}\"") != null;
            var pushed = GitHelper.RunGit(repoRoot, "push") != null;
            var version = GitHelper.CurrentVersion(repoRoot);
            if (version == null)
            {
                MessageBox.Show("バージョン情報の取得に失敗しました。", "Lolit");
                return;
            }

            try
            {
                SubmitAssemblyGraph(model, repoRoot, repoName, version, client);
            }
            catch (Exception ex)
            {
                MessageBox.Show($"メタデータの送信に失敗しました: {ex.Message}", "Lolit");
                return;
            }

            var pushNote = pushed ? "" : "\n(共有サーバーへの送信に失敗しました。ネットワーク接続を確認してください)";
            var commitNote = committed ? "" : "\n(変更がなかったため、既存のバージョンとして記録しました)";
            MessageBox.Show($"バージョン {version.Substring(0, Math.Min(8, version.Length))} として保存しました。{commitNote}{pushNote}", "Lolit");
        }

        /// <summary>
        /// Records the reference graph for the whole assembly tree in one
        /// go: the root's direct children, and -- recursively -- every
        /// sub-assembly's own direct children. All pinned to the same
        /// version, since "save" just committed everything together.
        /// </summary>
        private void SubmitAssemblyGraph(Model model, string repoRoot, string repoName, string version, LolitClient client)
        {
            var rootRelPath = GitHelper.RelativePath(repoRoot, model.FilePath);
            if (rootRelPath == null) return;

            var meta = ExtractMetadata(model);
            client.PostMetadata(repoName, rootRelPath, version, meta);

            if (!model.IsAssembly) return;

            var visitedAssemblies = new HashSet<string>();
            void SubmitNode(string nodeRelPath, IEnumerable<Component> children)
            {
                if (!visitedAssemblies.Add(nodeRelPath)) return;
                var deps = new List<DependencyRef>();
                foreach (var child in children)
                {
                    if (string.IsNullOrEmpty(child.FilePath)) continue;
                    var childRel = GitHelper.RelativePath(repoRoot, child.FilePath);
                    if (childRel == null) continue;
                    deps.Add(new DependencyRef { Path = childRel, Version = version });

                    // Recurse into sub-assemblies so the whole tree is captured
                    // in this single save, not just direct children.
                    if (child.Children != null && child.Children.Count > 0)
                        SubmitNode(childRel, child.Children);
                }
                if (deps.Count > 0)
                    client.PostDependencies(repoName, nodeRelPath, version, deps);
            }

            var topLevelChildren = model.Components()
                .Where(t => t.Item2 == 1)
                .Select(t => t.Item1);
            SubmitNode(rootRelPath, topLevelChildren);
        }

        private SWMetadata ExtractMetadata(Model model)
        {
            var massProps = model.MassProperties;
            double mass = massProps?.Mass ?? 0;
            double volume = massProps?.Volume ?? 0;

            var props = new Dictionary<string, object>();
            foreach (var prop in model.Extension.CustomPropertyEditor("").GetCustomProperties())
            {
                props[prop.Name] = prop.ResolvedValue;
            }

            var bom = new List<BomItem>();
            if (model.IsAssembly)
            {
                var counts = new Dictionary<string, int>();
                foreach (var (component, _depth) in model.Components())
                {
                    var name = Path.GetFileName(component.FilePath);
                    counts[name] = counts.TryGetValue(name, out var n) ? n + 1 : 1;
                }
                foreach (var kv in counts)
                {
                    bom.Add(new BomItem { Part = kv.Key, Qty = kv.Value });
                }
            }

            return new SWMetadata
            {
                // SolidWorks mass properties are always expressed in SI base units (kg, m^3).
                MassKg = mass,
                VolumeMm3 = volume * 1e9,
                Material = "",
                BOM = bom,
                CustomProperties = props
            };
        }

        // ---------- history / download ----------

        private void OnHistoryClicked()
        {
            var model = SolidWorksEnvironment.Application.ActiveModel;
            if (model == null)
            {
                MessageBox.Show("開いているファイルがありません。", "Lolit");
                return;
            }
            var repoRoot = GitHelper.FindRepoRoot(model.FilePath);
            var repoName = repoRoot != null ? GitHelper.RepoNameFromOrigin(repoRoot) : null;
            var relPath = repoRoot != null ? GitHelper.RelativePath(repoRoot, model.FilePath) : null;
            if (repoRoot == null || repoName == null || relPath == null)
            {
                MessageBox.Show("このファイルはLolitで管理されているフォルダの中にありません。", "Lolit");
                return;
            }
            var client = EnsureClient();
            if (client == null) return;

            List<VersionInfo> versions;
            try
            {
                versions = client.GetHistory(repoName, relPath);
            }
            catch (Exception ex)
            {
                MessageBox.Show($"履歴の取得に失敗しました: {ex.Message}", "Lolit");
                return;
            }
            if (versions.Count == 0)
            {
                MessageBox.Show("このファイルの保存履歴がまだありません。", "Lolit");
                return;
            }

            using var dlg = new HistoryDialog(Path.GetFileName(model.FilePath), versions, model.IsAssembly);
            dlg.DownloadRequested += (version, withDeps) => DownloadVersion(client, repoName, relPath, version, withDeps);
            dlg.ShowDialog();
        }

        private void DownloadVersion(LolitClient client, string repo, string path, VersionInfo version, bool withDependencies)
        {
            try
            {
                if (withDependencies)
                {
                    using var fbd = new FolderBrowserDialog { Description = "ダウンロード先フォルダを選択してください" };
                    if (fbd.ShowDialog() != DialogResult.OK) return;
                    var bundle = client.DownloadBundle(repo, path, version.Hash);
                    ExtractBundle(bundle, fbd.SelectedPath);
                    MessageBox.Show($"「{path}」と関連部品をダウンロードしました。", "Lolit");
                }
                else
                {
                    using var sfd = new SaveFileDialog { FileName = Path.GetFileName(path) };
                    if (sfd.ShowDialog() != DialogResult.OK) return;
                    var content = client.Download(repo, path, version.Hash);
                    File.WriteAllBytes(sfd.FileName, content);
                    MessageBox.Show("ダウンロードしました。", "Lolit");
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show($"ダウンロードに失敗しました: {ex.Message}", "Lolit");
            }
        }

        private static void ExtractBundle(byte[] zipBytes, string destinationFolder)
        {
            using var stream = new MemoryStream(zipBytes);
            using var archive = new ZipArchive(stream, ZipArchiveMode.Read);
            foreach (var entry in archive.Entries)
            {
                if (string.IsNullOrEmpty(entry.Name)) continue; // directory entry
                var destPath = Path.Combine(destinationFolder, entry.FullName.Replace('/', Path.DirectorySeparatorChar));
                Directory.CreateDirectory(Path.GetDirectoryName(destPath)!);
                entry.ExtractToFile(destPath, overwrite: true);
            }
        }
    }

    // Property names are pinned with JsonPropertyName so they line up with
    // lolit-server's db.SWMetadata json tags (mass_kg, volume_mm3, ...)
    // regardless of System.Text.Json's default naming policy.
    public class SWMetadata
    {
        [JsonPropertyName("mass_kg")]
        public double MassKg { get; set; }

        [JsonPropertyName("volume_mm3")]
        public double VolumeMm3 { get; set; }

        [JsonPropertyName("material")]
        public string Material { get; set; } = "";

        [JsonPropertyName("bom")]
        public List<BomItem> BOM { get; set; } = new();

        [JsonPropertyName("custom_properties")]
        public Dictionary<string, object> CustomProperties { get; set; } = new();
    }

    public class BomItem
    {
        [JsonPropertyName("part")]
        public string Part { get; set; } = "";

        [JsonPropertyName("qty")]
        public int Qty { get; set; }
    }
}
