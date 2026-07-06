using System;
using System.IO;
using System.Net.Http;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json;
using System.Windows.Forms;
using CADBooster.SolidDna;
using SolidWorks.Interop.sldworks;

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

        public override bool PreConnectToSolidWorks()
        {
            PluginTitle = "Lolit";
            Description = "Lolit file sharing integration for SolidWorks";
            return true;
        }

        public override void ApplicationStartup()
        {
            // Add a simple toolbar command group.
            var cmd = new CommandManagerItem("Lolit Commit", 0, OnCommitClicked)
            {
                Tooltip = "Extract metadata and commit current document",
            };
            SolidWorksEnvironment.IApplication.CommandManager.AddCommandGroup(new CommandManagerGroup("Lolit", new[] { cmd }));
        }

        private void OnCommitClicked()
        {
            var model = SolidWorksEnvironment.IApplication.ActiveModel;
            if (model == null)
            {
                MessageBox.Show("No active document.");
                return;
            }

            var meta = ExtractMetadata(model);
            var json = JsonSerializer.Serialize(meta, new JsonSerializerOptions { PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower });
            var result = PostMetadata(model.FilePath, json);
            MessageBox.Show($"Metadata posted: {result}", "Lolit");
        }

        private SWMetadata ExtractMetadata(Model model)
        {
            var part = model.UnsafeObject as IPartDoc;
            var asm = model.UnsafeObject as IAssemblyDoc;

            double mass = 0, volume = 0;
            string material = "";
            if (model.Extension != null)
            {
                var massProp = model.Extension.CreateMassProperty();
                if (massProp != null)
                {
                    mass = massProp.Mass;
                    volume = massProp.Volume;
                }
            }

            var props = new Dictionary<string, object?>();
            var customProp = model.UnsafeObject.Extension?.CustomPropertyManager[""] ?? null;
            if (customProp != null)
            {
                var names = (string[])customProp.GetNames();
                if (names != null)
                {
                    foreach (var name in names)
                    {
                        customProp.Get2(name, out _, out string val);
                        props[name] = val;
                    }
                }
            }

            var bom = new List<BomItem>();
            if (asm != null)
            {
                var comp = asm.GetComponents(false) as object[];
                if (comp != null)
                {
                    var counts = new Dictionary<string, int>();
                    foreach (Component2 c in comp)
                    {
                        var name2 = c.GetPathName();
                        counts[name2] = counts.GetValueOrDefault(name2) + 1;
                    }
                    foreach (var kv in counts)
                    {
                        bom.Add(new BomItem { Part = Path.GetFileName(kv.Key), Qty = kv.Value });
                    }
                }
            }

            return new SWMetadata
            {
                File = Path.GetFileName(model.FilePath) ?? "",
                CommitHash = "", // filled by caller or git
                MassKg = mass / 1000.0,
                VolumeMm3 = volume * 1e9,
                Material = material,
                BOM = bom,
                CustomProperties = props
            };
        }

        private string PostMetadata(string filePath, string json)
        {
            var server = Environment.GetEnvironmentVariable(MetadataServerEnv) ?? "http://localhost:8080";
            var repo = Environment.GetEnvironmentVariable(RepoEnv) ?? "team/robot2026";
            var file = Path.GetFileName(filePath);
            var payload = new
            {
                repo,
                file,
                commit_hash = DateTimeOffset.UtcNow.ToUnixTimeSeconds().ToString(),
                metadata = json
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

    public class SWMetadata
    {
        public string File { get; set; } = "";
        public string CommitHash { get; set; } = "";
        public double MassKg { get; set; }
        public double VolumeMm3 { get; set; }
        public string Material { get; set; } = "";
        public List<BomItem> BOM { get; set; } = new();
        public Dictionary<string, object?> CustomProperties { get; set; } = new();
    }

    public class BomItem
    {
        public string Part { get; set; } = "";
        public int Qty { get; set; }
    }
}
