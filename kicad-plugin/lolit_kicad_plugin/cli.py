"""Standalone CLI for KiCAD diff helper."""

from __future__ import annotations

import argparse
import os
from pathlib import Path

from .diff import diff_components, extract_components, components_from_git
from .metadata import post_kicad_diff


def main() -> None:
    parser = argparse.ArgumentParser(description="Lolit KiCAD helper")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_extract = sub.add_parser("extract", help="Extract components from a KiCAD file")
    p_extract.add_argument("path")

    p_diff = sub.add_parser("diff", help="Diff components between two commits")
    p_diff.add_argument("path")
    p_diff.add_argument("base")
    p_diff.add_argument("head")
    p_diff.add_argument("--repo", default=".")

    p_post = sub.add_parser("post", help="Post component diff to metadata server")
    p_post.add_argument("path")

    args = parser.parse_args()

    if args.cmd == "extract":
        comps = extract_components(args.path)
        print(f"Extracted {len(comps)} components")
        for ref, info in comps.items():
            print(f"  {ref}: {info}")

    elif args.cmd == "diff":
        old = components_from_git(args.repo, args.base, args.path)
        new = components_from_git(args.repo, args.head, args.path)
        result = diff_components(old, new)
        print(result)

    elif args.cmd == "post":
        comps = extract_components(args.path)
        server = os.environ.get("LOLIT_SERVER", "http://localhost:8080")
        repo = os.environ.get("LOLIT_REPO", "team/robot2026")
        post_kicad_diff(server, repo, Path(args.path).name, comps)
        print("posted")


if __name__ == "__main__":
    main()
