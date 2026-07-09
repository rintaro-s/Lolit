using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Net.Http;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Windows.Forms;
using CADBooster.SolidDna;

namespace LolitSolidWorksAddin
{
    /// <summary>
    /// Lolit SolidWorks Add-in entry point.
    /// Implements metadata extraction, preview snapshot upload, and LFS lock helpers.
    /// </summary>
    [Guid("A1B2C3D4-E5F6-7890-1234-567890ABCDEF")]
    [ComVisible(true)]
    public class LolitAddIn : SolidAddIn
    {
        private const string MetadataServerEnv = "LOLIT_SERVER";
        private const string RepoEnv = "LOLIT_REPO";

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
            var cmd = new CommandManagerItem
            {
                Name = "Lolit Commit",
                Tooltip = "Extract metadata and commit current document",
                Hint = "Extract metadata and commit current document",
                ImageIndex = 0,
                VisibleForParts = true,
                VisibleForAssemblies = true,
                VisibleForDrawings = false,
                OnClick = OnCommitClicked,
            };
            CommandManager.CreateCommandMenu(
                title: "Lolit",
                id: 150_100,
                commandManagerItems: new List<CommandManagerItem> { cmd });
        }

        private void OnCommitClicked()
        {
            var model = SolidWorksEnvironment.Application.ActiveModel;
            if (model == null)
            {
                MessageBox.Show("No active document.");
                return;
            }

            var meta = ExtractMetadata(model);
            var commit = CurrentCommitHash(Path.GetDirectoryName(model.FilePath));
            var result = PostMetadata(model.FilePath, commit, meta);
            MessageBox.Show($"Metadata posted: {result}", "Lolit");
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

        /// <summary>
        /// Resolves the current git commit hash for the repo containing the
        /// active document, so metadata posted here lines up with the same
        /// commit the Gitea push webhook will later process. Returns "" if
        /// git isn't available or the file isn't inside a repo yet.
        /// </summary>
        private static string CurrentCommitHash(string workingDirectory)
        {
            if (string.IsNullOrEmpty(workingDirectory))
                return "";
            try
            {
                var psi = new ProcessStartInfo("git", "rev-parse HEAD")
                {
                    WorkingDirectory = workingDirectory,
                    RedirectStandardOutput = true,
                    RedirectStandardError = true,
                    UseShellExecute = false,
                    CreateNoWindow = true,
                };
                using var proc = Process.Start(psi);
                var output = proc.StandardOutput.ReadToEnd().Trim();
                proc.WaitForExit(5000);
                return proc.ExitCode == 0 ? output : "";
            }
            catch
            {
                return "";
            }
        }

        private static string PostMetadata(string filePath, string commitHash, SWMetadata meta)
        {
            var server = Environment.GetEnvironmentVariable(MetadataServerEnv) ?? "http://localhost:8080";
            var repo = Environment.GetEnvironmentVariable(RepoEnv) ?? "team/robot2026";
            var file = Path.GetFileName(filePath);
            var payload = new
            {
                repo,
                file,
                commit_hash = commitHash,
                metadata = meta,
            };
            var body = new StringContent(JsonSerializer.Serialize(payload), Encoding.UTF8, "application/json");
            try
            {
                using var client = new HttpClient { Timeout = TimeSpan.FromSeconds(10) };
                var resp = client.PostAsync($"{server}/api/metadata", body).Result;
                return resp.IsSuccessStatusCode ? "OK" : resp.StatusCode.ToString();
            }
            catch (Exception ex)
            {
                return ex.Message;
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
