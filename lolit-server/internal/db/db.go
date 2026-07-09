package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// Store wraps SQLite access.
type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    path TEXT NOT NULL,
    file_type TEXT,
    latest_commit TEXT,
    locked_by TEXT,
    UNIQUE(repo, path)
);

CREATE TABLE IF NOT EXISTS sw_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL,
    commit_hash TEXT NOT NULL,
    mass_kg REAL,
    volume_mm3 REAL,
    material TEXT,
    bom_json TEXT,
    properties_json TEXT,
    FOREIGN KEY(file_id) REFERENCES files(id)
);

CREATE TABLE IF NOT EXISTS kicad_diff (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL,
    commit_hash TEXT NOT NULL,
    diff_json TEXT NOT NULL,
    FOREIGN KEY(file_id) REFERENCES files(id)
);

CREATE TABLE IF NOT EXISTS previews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id INTEGER NOT NULL,
    commit_hash TEXT NOT NULL,
    image_path TEXT NOT NULL,
    FOREIGN KEY(file_id) REFERENCES files(id)
);

CREATE TABLE IF NOT EXISTS commits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    hash TEXT NOT NULL,
    message TEXT,
    author TEXT,
    ts INTEGER,
    UNIQUE(repo, hash)
);

CREATE TABLE IF NOT EXISTS releases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    tag TEXT NOT NULL,
    commit_hash TEXT NOT NULL,
    note TEXT,
    ts INTEGER,
    UNIQUE(repo, tag)
);

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    role TEXT NOT NULL DEFAULT 'member',
    created_at INTEGER
);

-- dependencies records, for an assembly (or sub-assembly) saved at a given
-- version, exactly which other file+version pairs it needs to open
-- correctly (e.g. a SolidWorks assembly's referenced parts). Recorded by
-- the client (SolidWorks Add-in) at save time, since only the client can
-- read the assembly's own component tree. This is what lets Lolit hand back
-- a self-consistent bundle for "this module at this version", the same way
-- a lockfile pins transitive versions.
CREATE TABLE IF NOT EXISTS dependencies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    path TEXT NOT NULL,
    version TEXT NOT NULL,
    dep_path TEXT NOT NULL,
    dep_version TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dependencies_lookup ON dependencies(repo, path, version);

CREATE INDEX IF NOT EXISTS idx_files_repo ON files(repo);
CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
CREATE INDEX IF NOT EXISTS idx_commits_repo ON commits(repo);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertFile inserts or updates a file record and returns its id.
func (s *Store) UpsertFile(repo, path, fileType, latestCommit string) (int64, error) {
	var id int64
	row := s.db.QueryRow(
		`INSERT INTO files (repo, path, file_type, latest_commit) VALUES (?, ?, ?, ?)
		 ON CONFLICT(repo, path) DO UPDATE SET file_type=excluded.file_type, latest_commit=excluded.latest_commit
		 RETURNING id`, repo, path, fileType, latestCommit)
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) GetFile(repo, path string) (*File, error) {
	row := s.db.QueryRow(`SELECT id, repo, path, file_type, latest_commit, locked_by FROM files WHERE repo=? AND path=?`, repo, path)
	f := &File{}
	var lock sql.NullString
	err := row.Scan(&f.ID, &f.Repo, &f.Path, &f.FileType, &f.LatestCommit, &lock)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.LockedBy = lock.String
	return f, nil
}

func (s *Store) ListFiles(repo, prefix string) ([]File, error) {
	query := `SELECT id, repo, path, file_type, latest_commit, locked_by FROM files WHERE repo=?`
	args := []interface{}{repo}
	if prefix != "" {
		query += ` AND path LIKE ?`
		args = append(args, prefix+"%")
	}
	query += ` ORDER BY path`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		var lock sql.NullString
		if err := rows.Scan(&f.ID, &f.Repo, &f.Path, &f.FileType, &f.LatestCommit, &lock); err != nil {
			return nil, err
		}
		f.LockedBy = lock.String
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) SetLock(repo, path, user string, lock bool) error {
	var lockedBy interface{}
	if lock {
		lockedBy = user
	} else {
		lockedBy = nil
	}
	_, err := s.db.Exec(`UPDATE files SET locked_by=? WHERE repo=? AND path=?`, lockedBy, repo, path)
	return err
}

func (s *Store) ListLocks(repo string) ([]File, error) {
	query := `SELECT id, repo, path, file_type, latest_commit, locked_by FROM files WHERE locked_by IS NOT NULL`
	args := []interface{}{}
	if repo != "" {
		query += ` AND repo=?`
		args = append(args, repo)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		var lock sql.NullString
		if err := rows.Scan(&f.ID, &f.Repo, &f.Path, &f.FileType, &f.LatestCommit, &lock); err != nil {
			return nil, err
		}
		f.LockedBy = lock.String
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) SaveCommit(repo, hash, message, author string, ts int64) error {
	_, err := s.db.Exec(`INSERT INTO commits (repo, hash, message, author, ts) VALUES (?, ?, ?, ?, ?)
	 ON CONFLICT(repo, hash) DO UPDATE SET message=excluded.message, author=excluded.author, ts=excluded.ts`,
		repo, hash, message, author, ts)
	return err
}

func (s *Store) RecentCommits(repo string, limit int) ([]Commit, error) {
	query := `SELECT repo, hash, message, author, ts FROM commits`
	args := []interface{}{}
	if repo != "" {
		query += ` WHERE repo=?`
		args = append(args, repo)
	}
	query += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Commit
	for rows.Next() {
		var c Commit
		if err := rows.Scan(&c.Repo, &c.Hash, &c.Message, &c.Author, &c.TS); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SaveSWMetadata(fileID int64, commit string, meta SWMetadata) error {
	bom, _ := json.Marshal(meta.BOM)
	props, _ := json.Marshal(meta.Properties)
	_, err := s.db.Exec(`INSERT INTO sw_metadata (file_id, commit_hash, mass_kg, volume_mm3, material, bom_json, properties_json)
	 VALUES (?, ?, ?, ?, ?, ?, ?)`, fileID, commit, meta.MassKg, meta.VolumeMm3, meta.Material, bom, props)
	return err
}

func (s *Store) GetSWMetadata(fileID int64, commit string) (*SWMetadata, error) {
	row := s.db.QueryRow(`SELECT mass_kg, volume_mm3, material, bom_json, properties_json FROM sw_metadata
	 WHERE file_id=? AND commit_hash=? ORDER BY id DESC LIMIT 1`, fileID, commit)
	var m SWMetadata
	var bomJSON, propsJSON string
	err := row.Scan(&m.MassKg, &m.VolumeMm3, &m.Material, &bomJSON, &propsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(bomJSON), &m.BOM)
	json.Unmarshal([]byte(propsJSON), &m.Properties)
	return &m, nil
}

func (s *Store) SaveKiCadDiff(fileID int64, commit string, diff interface{}) error {
	b, _ := json.Marshal(diff)
	_, err := s.db.Exec(`INSERT INTO kicad_diff (file_id, commit_hash, diff_json) VALUES (?, ?, ?)`, fileID, commit, b)
	return err
}

func (s *Store) GetKiCadDiff(fileID int64, commit string) (string, error) {
	row := s.db.QueryRow(`SELECT diff_json FROM kicad_diff WHERE file_id=? AND commit_hash=? ORDER BY id DESC LIMIT 1`, fileID, commit)
	var d string
	err := row.Scan(&d)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return d, err
}

func (s *Store) SavePreview(fileID int64, commit, imagePath string) error {
	_, err := s.db.Exec(`INSERT INTO previews (file_id, commit_hash, image_path) VALUES (?, ?, ?)`, fileID, commit, imagePath)
	return err
}

func (s *Store) GetPreview(fileID int64, commit string) (string, error) {
	row := s.db.QueryRow(`SELECT image_path FROM previews WHERE file_id=? AND commit_hash=? ORDER BY id DESC LIMIT 1`, fileID, commit)
	var p string
	err := row.Scan(&p)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return p, err
}

func (s *Store) CreateRelease(repo, tag, commit, note string, ts int64) error {
	_, err := s.db.Exec(`INSERT INTO releases (repo, tag, commit_hash, note, ts) VALUES (?, ?, ?, ?, ?)
	 ON CONFLICT(repo, tag) DO UPDATE SET commit_hash=excluded.commit_hash, note=excluded.note, ts=excluded.ts`,
		repo, tag, commit, note, ts)
	return err
}

func (s *Store) ListReleases(repo string) ([]Release, error) {
	rows, err := s.db.Query(`SELECT repo, tag, commit_hash, note, ts FROM releases WHERE repo=? ORDER BY ts DESC`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.Repo, &r.Tag, &r.CommitHash, &r.Note, &r.TS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// User accounts (Lolit's own lightweight accounts, separate from Gitea's:
// they authenticate the WebUI/API and attribute locks, uploads and admin
// actions to a person, while Gitea keeps owning raw git/LFS auth).

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`
}

const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash, displayName, role string, createdAt int64) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, display_name, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		username, passwordHash, displayName, role, createdAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, display_name, role, created_at FROM users WHERE username=?`, username)
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByID(id int64) (*User, error) {
	row := s.db.QueryRow(`SELECT id, username, password_hash, display_name, role, created_at FROM users WHERE id=?`, id)
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, password_hash, display_name, role, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserRole(id int64, role string) error {
	_, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, role, id)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

// Dependency is one edge: `path` at `version` requires `dep_path` at
// exactly `dep_version` to open correctly.
type Dependency struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	DepPath    string `json:"dep_path"`
	DepVersion string `json:"dep_version"`
}

// SaveDependencies replaces the recorded dependency edges for (repo, path,
// version) -- a re-save of the same version fully overwrites the previous
// edge list, since the whole point is "this is the exact reference graph as
// of this version" and stale extra edges would corrupt bundle resolution.
func (s *Store) SaveDependencies(repo, path, version string, deps []Dependency) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM dependencies WHERE repo=? AND path=? AND version=?`, repo, path, version); err != nil {
		return err
	}
	for _, d := range deps {
		if _, err := tx.Exec(`INSERT INTO dependencies (repo, path, version, dep_path, dep_version) VALUES (?, ?, ?, ?, ?)`,
			repo, path, version, d.DepPath, d.DepVersion); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetDependencies returns the direct dependency edges recorded for (repo,
// path, version).
func (s *Store) GetDependencies(repo, path, version string) ([]Dependency, error) {
	rows, err := s.db.Query(`SELECT path, version, dep_path, dep_version FROM dependencies WHERE repo=? AND path=? AND version=?`, repo, path, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dependency
	for rows.Next() {
		var d Dependency
		if err := rows.Scan(&d.Path, &d.Version, &d.DepPath, &d.DepVersion); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Exec exposes raw exec for migrations/tests.
func (s *Store) Exec(sql string, args ...interface{}) (sql.Result, error) {
	return s.db.Exec(sql, args...)
}

// Query exposes raw query for migrations/tests.
func (s *Store) Query(sql string, args ...interface{}) (*sql.Rows, error) {
	return s.db.Query(sql, args...)
}

func (s *Store) DB() *sql.DB { return s.db }

// Models.

type File struct {
	ID           int64  `json:"id"`
	Repo         string `json:"repo"`
	Path         string `json:"path"`
	FileType     string `json:"file_type"`
	LatestCommit string `json:"latest_commit"`
	LockedBy     string `json:"locked_by,omitempty"`
}

type Commit struct {
	Repo    string `json:"repo"`
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	TS      int64  `json:"ts"`
}

type SWMetadata struct {
	MassKg     float64                `json:"mass_kg"`
	VolumeMm3  float64                `json:"volume_mm3"`
	Material   string                 `json:"material"`
	BOM        []BOMItem              `json:"bom"`
	Properties map[string]interface{} `json:"custom_properties"`
}

type BOMItem struct {
	Part string `json:"part"`
	Qty  int    `json:"qty"`
}

type Release struct {
	Repo       string `json:"repo"`
	Tag        string `json:"tag"`
	CommitHash string `json:"commit_hash"`
	Note       string `json:"note"`
	TS         int64  `json:"ts"`
}

// FileTypeFromPath guesses file type from extension (case-insensitive).
func FileTypeFromPath(path string) string {
	ext := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = strings.ToLower(path[i:])
			break
		}
		if path[i] == '/' {
			break
		}
	}
	switch ext {
	case ".sldprt":
		return "sldprt"
	case ".sldasm":
		return "sldasm"
	case ".kicad_pcb":
		return "kicad_pcb"
	case ".kicad_sch":
		return "kicad_sch"
	case ".step", ".stp":
		return "step"
	case ".stl":
		return "stl"
	default:
		return "other"
	}
}

func (s *Store) DebugDump() {
	fmt.Println("files:")
	rows, _ := s.Query(`SELECT repo, path, file_type FROM files`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var repo, path, ft string
			rows.Scan(&repo, &path, &ft)
			fmt.Println(" ", repo, path, ft)
		}
	}
}
