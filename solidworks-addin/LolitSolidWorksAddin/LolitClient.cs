using System;
using System.Collections.Generic;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace LolitSolidWorksAddin
{
    internal class VersionInfo
    {
        [JsonPropertyName("hash")] public string Hash { get; set; } = "";
        [JsonPropertyName("author")] public string Author { get; set; } = "";
        [JsonPropertyName("message")] public string Message { get; set; } = "";
        [JsonPropertyName("ts")] public long Ts { get; set; }
    }

    internal class DependencyRef
    {
        [JsonPropertyName("path")] public string Path { get; set; } = "";
        [JsonPropertyName("version")] public string Version { get; set; } = "";
    }

    /// <summary>Thin HTTP client for the Lolit Metadata Server API.</summary>
    internal class LolitClient
    {
        public string Server { get; }
        public string Token { get; set; }
        private readonly HttpClient _http = new HttpClient { Timeout = TimeSpan.FromSeconds(60) };

        public LolitClient(string server, string token)
        {
            Server = server.TrimEnd('/');
            Token = token;
        }

        private HttpRequestMessage NewRequest(HttpMethod method, string path)
        {
            var req = new HttpRequestMessage(method, Server + path);
            if (!string.IsNullOrEmpty(Token))
                req.Headers.Authorization = new AuthenticationHeaderValue("Bearer", Token);
            return req;
        }

        public string Login(string username, string password)
        {
            var body = JsonSerializer.Serialize(new { username, password });
            var req = new HttpRequestMessage(HttpMethod.Post, Server + "/api/auth/login")
            {
                Content = new StringContent(body, Encoding.UTF8, "application/json")
            };
            var resp = _http.SendAsync(req).Result;
            var text = resp.Content.ReadAsStringAsync().Result;
            if (!resp.IsSuccessStatusCode)
                throw new Exception(ExtractError(text) ?? resp.StatusCode.ToString());
            using var doc = JsonDocument.Parse(text);
            Token = doc.RootElement.GetProperty("token").GetString() ?? "";
            return Token;
        }

        public List<VersionInfo> GetHistory(string repo, string path)
        {
            var req = NewRequest(HttpMethod.Get, $"/api/history?repo={Uri.EscapeDataString(repo)}&path={Uri.EscapeDataString(path)}");
            var resp = _http.SendAsync(req).Result;
            var text = resp.Content.ReadAsStringAsync().Result;
            if (!resp.IsSuccessStatusCode)
                throw new Exception(ExtractError(text) ?? resp.StatusCode.ToString());
            return JsonSerializer.Deserialize<List<VersionInfo>>(text) ?? new List<VersionInfo>();
        }

        public byte[] Download(string repo, string path, string version)
        {
            var req = NewRequest(HttpMethod.Get,
                $"/api/download?repo={Uri.EscapeDataString(repo)}&path={Uri.EscapeDataString(path)}&version={Uri.EscapeDataString(version)}");
            var resp = _http.SendAsync(req).Result;
            if (!resp.IsSuccessStatusCode)
                throw new Exception(ExtractError(resp.Content.ReadAsStringAsync().Result) ?? resp.StatusCode.ToString());
            return resp.Content.ReadAsByteArrayAsync().Result;
        }

        public byte[] DownloadBundle(string repo, string path, string version)
        {
            var req = NewRequest(HttpMethod.Get,
                $"/api/download-bundle?repo={Uri.EscapeDataString(repo)}&path={Uri.EscapeDataString(path)}&version={Uri.EscapeDataString(version)}");
            var resp = _http.SendAsync(req).Result;
            if (!resp.IsSuccessStatusCode)
                throw new Exception(ExtractError(resp.Content.ReadAsStringAsync().Result) ?? resp.StatusCode.ToString());
            return resp.Content.ReadAsByteArrayAsync().Result;
        }

        public void PostMetadata(string repo, string path, string version, SWMetadata meta)
        {
            Post("/api/metadata", new { repo, file = path, commit_hash = version, metadata = meta });
        }

        public void PostDependencies(string repo, string path, string version, List<DependencyRef> deps)
        {
            Post("/api/dependencies", new { repo, path, version, deps });
        }

        private void Post(string path, object payload)
        {
            var body = JsonSerializer.Serialize(payload);
            var req = NewRequest(HttpMethod.Post, path);
            req.Content = new StringContent(body, Encoding.UTF8, "application/json");
            var resp = _http.SendAsync(req).Result;
            if (!resp.IsSuccessStatusCode)
                throw new Exception(ExtractError(resp.Content.ReadAsStringAsync().Result) ?? resp.StatusCode.ToString());
        }

        private static string? ExtractError(string json)
        {
            try
            {
                using var doc = JsonDocument.Parse(json);
                if (doc.RootElement.TryGetProperty("error", out var e)) return e.GetString();
            }
            catch
            {
                // not JSON; fall through
            }
            return null;
        }
    }
}
