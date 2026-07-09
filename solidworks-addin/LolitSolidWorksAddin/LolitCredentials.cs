using System;
using System.IO;
using System.Text.Json;

namespace LolitSolidWorksAddin
{
    /// <summary>Persists the Lolit login session (separate from Git/Gitea credentials) for this machine/user.</summary>
    internal class LolitCredentials
    {
        public string Server { get; set; } = "";
        public string Token { get; set; } = "";

        private static string FilePath()
        {
            var dir = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "Lolit");
            Directory.CreateDirectory(dir);
            return Path.Combine(dir, "credentials.json");
        }

        public static LolitCredentials? Load()
        {
            try
            {
                var path = FilePath();
                if (!File.Exists(path)) return null;
                return JsonSerializer.Deserialize<LolitCredentials>(File.ReadAllText(path));
            }
            catch
            {
                return null;
            }
        }

        public void Save()
        {
            File.WriteAllText(FilePath(), JsonSerializer.Serialize(this));
        }

        public static void Clear()
        {
            try { File.Delete(FilePath()); } catch { }
        }
    }
}
