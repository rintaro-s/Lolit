using System;
using System.Diagnostics;
using System.IO;

namespace LolitSolidWorksAddin
{
    /// <summary>
    /// Lolit stores its data in a Gitea-backed repository purely as an
    /// implementation detail (building a bespoke backend from scratch wasn't
    /// worth it) -- these helpers exist only to find "which Lolit-managed
    /// folder is this file in" and "what's its current version", never to
    /// expose Git concepts to the user.
    /// </summary>
    internal static class GitHelper
    {
        /// <summary>Walks up from a file path to find the repo root (the directory containing ".git").</summary>
        public static string? FindRepoRoot(string startPath)
        {
            var dir = new DirectoryInfo(Path.GetDirectoryName(Path.GetFullPath(startPath)) ?? startPath);
            while (dir != null)
            {
                if (Directory.Exists(Path.Combine(dir.FullName, ".git")))
                    return dir.FullName;
                dir = dir.Parent;
            }
            return null;
        }

        /// <summary>
        /// Returns fullPath relative to repoRoot using forward slashes (the
        /// form Lolit's server APIs expect), or null if fullPath isn't
        /// actually inside repoRoot.
        /// </summary>
        public static string? RelativePath(string repoRoot, string fullPath)
        {
            var root = Path.GetFullPath(repoRoot).TrimEnd(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
            var full = Path.GetFullPath(fullPath);
            if (!full.StartsWith(root, StringComparison.OrdinalIgnoreCase))
                return null;
            var rel = full.Substring(root.Length).TrimStart(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
            return rel.Length == 0 ? null : rel.Replace('\\', '/');
        }

        public static string? RunGit(string repoRoot, string args)
        {
            try
            {
                var psi = new ProcessStartInfo("git", args)
                {
                    WorkingDirectory = repoRoot,
                    RedirectStandardOutput = true,
                    RedirectStandardError = true,
                    UseShellExecute = false,
                    CreateNoWindow = true,
                };
                using var proc = Process.Start(psi);
                if (proc == null) return null;
                var output = proc.StandardOutput.ReadToEnd().Trim();
                proc.WaitForExit(20000);
                return proc.ExitCode == 0 ? output : null;
            }
            catch
            {
                return null;
            }
        }

        /// <summary>Lolit's "repo name" (owner/repo) is the last two path segments of the git remote URL.</summary>
        public static string? RepoNameFromOrigin(string repoRoot)
        {
            var rawOrigin = RunGit(repoRoot, "remote get-url origin");
            if (string.IsNullOrEmpty(rawOrigin)) return null;
            var origin = rawOrigin!.TrimEnd('/');
            if (origin.EndsWith(".git")) origin = origin.Substring(0, origin.Length - 4);
            origin = origin.Replace(':', '/'); // scp-like "git@host:owner/repo"
            var parts = origin.Split('/');
            if (parts.Length < 2) return null;
            return parts[parts.Length - 2] + "/" + parts[parts.Length - 1];
        }

        public static string? CurrentVersion(string repoRoot) => RunGit(repoRoot, "rev-parse HEAD");
    }
}
