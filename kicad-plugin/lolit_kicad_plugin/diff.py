"""KiCAD S-expression component extraction and diff.

Module name is `diff` to match imports.
"""

from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Any

from sexpdata import loads


def extract_components(path: str) -> dict[str, dict[str, Any]]:
    """Extract components from a .kicad_pcb or .kicad_sch file.

    Returns a mapping from reference designator to component attributes.
    """
    text = Path(path).read_text(encoding="utf-8")
    tree = loads(text)
    components: dict[str, dict[str, Any]] = {}
    for node in tree:
        if not isinstance(node, list):
            continue
        head = _sym_str(node[0])
        if head not in ("footprint", "symbol"):
            continue
        ref = _find_reference(node)
        if ref is None:
            continue
        components[ref] = {
            "footprint": _footprint_name(node),
            "value": _find_property(node, "value"),
            "nets": _collect_nets(node),
        }
    return components


def diff_components(
    old: dict[str, dict[str, Any]], new: dict[str, dict[str, Any]]
) -> dict[str, Any]:
    """Compare two component dictionaries."""
    added = []
    removed = []
    changed = []
    for ref, comp in new.items():
        if ref not in old:
            added.append({"ref": ref, **comp})
        elif old[ref] != comp:
            changed.append({"ref": ref, "before": old[ref], "after": comp})
    for ref, comp in old.items():
        if ref not in new:
            removed.append({"ref": ref, **comp})
    return {"added": added, "removed": removed, "changed": changed}


def components_from_git(repo: str, commit: str, path: str) -> dict[str, dict[str, Any]]:
    """Extract components from a file at a given git commit."""
    data = subprocess.check_output(["git", "show", f"{commit}:{path}"], cwd=repo)
    tree = loads(data.decode("utf-8"))
    components: dict[str, dict[str, Any]] = {}
    for node in tree:
        if not isinstance(node, list):
            continue
        if _sym_str(node[0]) not in ("footprint", "symbol"):
            continue
        ref = _find_reference(node)
        if ref is None:
            continue
        components[ref] = {
            "footprint": _footprint_name(node),
            "value": _find_property(node, "value"),
            "nets": _collect_nets(node),
        }
    return components


def _footprint_name(node: list[Any]) -> str | None:
    """Extract footprint name from (footprint \"NAME\" ...)."""
    if len(node) >= 2 and isinstance(node[1], str):
        return node[1]
    return None


def _find_reference(node: list[Any]) -> str | None:
    """Find reference designator inside a footprint/symbol sexp."""
    for child in node:
        if not isinstance(child, list):
            continue
        tag = _sym_str(child[0])
        if tag == "fp_text" and len(child) >= 2 and _sym_str(child[1]) == "reference":
            value = child[2]
            if isinstance(value, str):
                return value
        if tag == "property" and len(child) >= 2:
            key = _sym_str(child[1])
            if isinstance(key, str) and key.lower() == "reference" and len(child) >= 3:
                value = child[2]
                if isinstance(value, str):
                    return value
    return None


def _find_property(node: list[Any], key: str) -> str | None:
    for child in node:
        if not isinstance(child, list):
            continue
        tag = _sym_str(child[0])
        if tag in ("fp_text", "property") and len(child) >= 2 and _sym_str(child[1]) == key:
            value = child[2]
            return value if isinstance(value, str) else None
    return None


def _collect_nets(node: list[Any]) -> list[str]:
    nets: set[str] = set()
    for child in node:
        if not isinstance(child, list):
            continue
        if _sym_str(child[0]) == "pad" and len(child) >= 4:
            for sub in child:
                if isinstance(sub, list) and _sym_str(sub[0]) == "net" and len(sub) >= 2:
                    nets.add(str(sub[1]))
    return sorted(nets)


def _sym_str(value: Any) -> str:
    """Convert a sexpdata Symbol or string to plain string."""
    return str(value)
