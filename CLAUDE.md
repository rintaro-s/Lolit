# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

Lolit is a file-sharing system for robotics competition ("ロボコン") dev teams. It uses Git + Gitea as
the VCS core and layers on CAD-specific (SolidWorks / KiCAD) diffing, search, and file locking. It is
meant for development-time use only (school/home LAN), not competition venues.

Git/Git LFS/LFS-lock/auth are entirely delegated to a self-hosted Gitea instance. This repo implements
everything around that: a metadata server that reacts to Gitea push webhooks, a CLI wrapper, and
client-side plugins for SolidWorks and KiCAD. See `spec.md` (Japanese) for the full spec and
`docs/setup.md` for deployment/setup instructions — read these for details beyond this file.

## Repo layout

- `lolit-server/` — Go metadata server (webhook receiver, REST API, WebSocket, WebUI host)
- `rv/` — Go CLI, a thin wrapper around `git`/`git-lfs` plus Lolit-specific commands
- `solidworks-addin/` — C# SolidWorks add-in (.NET Framework 4.8) that extracts metadata client-side
- `kicad-plugin/` — Python KiCAD plugin/CLI for component/net diffing of `.kicad_pcb`/`.kicad_sch`
- `docs/setup.md` — Raspberry Pi + Gitea deployment guide
- `InteractiveHtmlBom/`, `SolidDNA/`, `kicad-diff-visualizer/` — vendored **reference-only** repos
  (gitignored, not part of the product; consulted for prior art on SolidWorks/KiCAD integration)

## Build & test commands

From repo root (`Makefile`):
```bash
make server   # cd lolit-server && go build -o lolit-server .
make cli      # cd rv && go build -o rv .
make test     # go test ./... in both lolit-server and rv
make clean
```

Per-component:
```bash
# lolit-server
cd lolit-server && go build -o lolit-server . && LOLIT_DATA_DIR=/tmp/lolit-test ./lolit-server
cd lolit-server && go test ./...
cd lolit-server && go test ./internal/db/...        # single package
cd lolit-server && go test ./internal/db/... -run TestName

# rv CLI
cd rv && go build -o rv . && ./rv version
cd rv && go test ./...

# kicad-plugin (Python)
cd kicad-plugin && pip install -e .
python -m lolit_kicad_plugin.cli extract <path/to/file.kicad_pcb>
python -m lolit_kicad_plugin.cli diff <path> <base-commit> <head-commit> --repo .

# solidworks-addin
# Windows-only: open solidworks-addin/LolitSolidWorksAddin.csproj in Visual Studio, build with .NET Framework 4.8
```

There is no build/test workflow for `InteractiveHtmlBom/`, `SolidDNA/`, `kicad-diff-visualizer/` in this
context — they are external reference repos excluded via `.gitignore`.

## Architecture

```
Client
 ├── rv CLI (Go)                — git/git-lfs wrapper + lock/search/history/release commands
 ├── WebUI (HTMX + Alpine.js)   — served by lolit-server, Google-Drive-style browser UI
 ├── SolidWorks Add-in (C#)     — extracts BOM/mass/material/custom-properties, since SolidWorks
 │                                 APIs are only callable client-side, never from the server
 └── KiCAD Plugin (Python)      — extracts component/net info from .kicad_pcb/.kicad_sch via sexpdata
        ↓ REST / WebSocket
Gitea (Git + Git LFS + LFS Lock + JWT auth)  — owns all VCS/lock/auth concerns
        ↓ push webhook (POST /webhook)
Lolit Metadata Server (Go, lolit-server/)
 ├── internal/webhook — receives Gitea push payloads, walks changed files, dispatches by extension
 ├── internal/gitutil — shells out to git to inspect repos/diffs
 ├── internal/db      — SQLite-backed metadata store (commits, files, locks, releases, KiCAD diffs...)
 ├── internal/search  — Bleve full-text index, updated on every push
 ├── internal/api     — REST endpoints consumed by the WebUI (/api/files, /api/commits, /api/locks,
 │                       /api/search, /api/releases, /api/metadata, /api/history, ...)
 ├── internal/ws      — WebSocket hub, used to push live lock-state notifications to clients
 └── web/webui/        — HTMX/Alpine templates + static assets, embedded into the Go binary via
                          //go:embed but served from disk instead when present (dev convenience —
                          see getWebFS()/serveIndex() in main.go)
        ↓
ZFS storage (or plain ext4 USB drive) mounted on a Raspberry Pi, holding Gitea's bare repos + LFS objects
```

Key design decisions worth knowing before changing things:

- **Gitea owns Git/LFS/locking/auth entirely.** lolit-server never implements its own VCS, lock, or
  auth logic — it only reacts to webhooks and calls out to `git` (via `internal/gitutil`) for diffs.
- **File-type dispatch drives the metadata pipeline.** On push, changed files are routed by extension:
  `.kicad_pcb`/`.kicad_sch` → KiCAD component/net diff (S-expression parse, matched by reference
  designator like `R1`/`C3`/`U2`); `.SLDPRT`/`.SLDASM` → SolidWorks metadata (produced client-side by
  the add-in, since the server has no SolidWorks API access); `.STEP`/`.STL` → preview generation only;
  everything else → full-text index update only.
- **No 3-way merge for KiCAD files.** Concurrent edits are prevented via Git LFS Lock (one editor per
  file at a time), not resolved via merging.
- **No native GUI client.** The WebUI is the single cross-platform GUI; `rv` is the CLI; SolidWorks/
  KiCAD get first-class plugins because those tools can't be scripted purely from the outside.
- **Spec intentionally under-constrains behavior.** `spec.md` explicitly asks implementers to fill in
  unspecified details themselves and to keep the UX permissive (e.g., "just drag a folder in" should
  work without forcing users through detailed workflows) — prefer simple, forgiving defaults over
  rigid validation when a requirement isn't pinned down.
