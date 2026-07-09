"""Client for Lolit Metadata Server from KiCAD."""

from __future__ import annotations

from typing import Any

import requests


def post_kicad_diff(server: str, repo: str, path: str, commit: str, diff: dict[str, Any]) -> None:
    """Post a component diff (added/removed/changed) to the metadata server.

    `commit` must be the real git commit hash the diff is associated with, so
    it lines up with the same file/commit pair the Gitea push webhook uses.
    """
    url = f"{server}/api/kicad-diff"
    payload = {
        "repo": repo,
        "path": path,
        "commit": commit,
        "diff": diff,
    }
    resp = requests.post(url, json=payload, timeout=10)
    resp.raise_for_status()
