"""Client for Lolit Metadata Server from KiCAD."""

from __future__ import annotations

import os
import requests


def post_kicad_diff(server: str, repo: str, filename: str, components: dict) -> None:
    """Post a component list/diff to the metadata server."""
    url = f"{server}/api/metadata"
    payload = {
        "repo": repo,
        "file": filename,
        "commit_hash": "kicad-plugin-" + os.environ.get("USER", "unknown"),
        "metadata": {
            "components": components,
            "source": "kicad_plugin",
        },
    }
    resp = requests.post(url, json=payload, timeout=10)
    resp.raise_for_status()
