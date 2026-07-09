package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// credentials is what loli persists locally after `loli login`, so later
// commands (lock/search/release/upload) can authenticate without asking
// again every time.
type credentials struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func credentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lolit", "credentials.json"), nil
}

func loadCredentials() *credentials {
	path, err := credentialsPath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil
	}
	if c.Server != serverURL() {
		// Credentials were saved for a different LOLIT_SERVER; don't send a
		// stale token to the wrong host.
		return nil
	}
	return &c
}

func saveCredentials(c credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func clearCredentials() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func authToken() string {
	if c := loadCredentials(); c != nil {
		return c.Token
	}
	return ""
}

func runLogin(args []string) {
	username := ""
	if len(args) > 0 {
		username = args[0]
	}
	reader := bufio.NewReader(os.Stdin)
	if username == "" {
		fmt.Print("ユーザー名: ")
		line, _ := reader.ReadString('\n')
		username = strings.TrimSpace(line)
	}
	// LOLIT_PASSWORD lets `loli login` run non-interactively (scripted setup,
	// CI); otherwise prompt with the terminal's echo disabled.
	password := os.Getenv("LOLIT_PASSWORD")
	if password == "" {
		fmt.Print("パスワード: ")
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, "パスワードの読み取りに失敗しました（対話端末が必要です。スクリプトから使う場合は LOLIT_PASSWORD を設定してください）:", err)
			os.Exit(1)
		}
		password = string(passwordBytes)
	}

	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, err := http.NewRequest("POST", serverURL()+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "サーバーに接続できませんでした:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
		Error string `json:"error"`
		User  struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
		} `json:"user"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusOK {
		if result.Error == "" {
			result.Error = resp.Status
		}
		fmt.Fprintln(os.Stderr, "ログインに失敗しました:", result.Error)
		os.Exit(1)
	}

	if err := saveCredentials(credentials{Server: serverURL(), Token: result.Token}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not save credentials:", err)
	}
	fmt.Printf("%s としてログインしました (%s)\n", result.User.Username, result.User.Role)
}

func runLogout() {
	if err := clearCredentials(); err != nil {
		fmt.Fprintln(os.Stderr, "logout:", err)
		os.Exit(1)
	}
	fmt.Println("ログアウトしました")
}

func runWhoami() {
	req, err := mustReq("GET", "/api/auth/me", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "whoami:", err)
		os.Exit(1)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "サーバーに接続できませんでした:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Println("ログインしていません。`loli login` を実行してください。")
		os.Exit(1)
	}
	var user struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	json.NewDecoder(resp.Body).Decode(&user)
	fmt.Printf("%s (%s) - %s\n", user.Username, user.DisplayName, user.Role)
}
