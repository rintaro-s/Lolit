"""Lolit KiCAD Plugin.

Provides a pcbnew action plugin to commit the current board/schematic,
extract component diff, and interact with the Lolit Metadata Server.
"""

import os

from .diff import extract_components
from .metadata import post_kicad_diff


SERVER = os.environ.get("LOLIT_SERVER", "http://localhost:8080")
REPO = os.environ.get("LOLIT_REPO", "team/robot2026")


def _register_plugin():
    """Register the KiCAD action plugin when running inside pcbnew."""
    try:
        import pcbnew
        import wx
    except ImportError:
        return

    class LolitActionPlugin(pcbnew.ActionPlugin):
        def defaults(self):
            self.name = "Lolit Commit"
            self.category = "Lolit"
            self.description = "Commit board to Lolit and send component diff"
            self.show_toolbar_button = True

        def Run(self):
            board = pcbnew.GetBoard()
            if board is None:
                wx.MessageBox("No board is open.", "Lolit")
                return

            path = board.GetFileName()
            if not path:
                wx.MessageBox("Board has no file name.", "Lolit")
                return

            current = extract_components(path)
            wx.MessageBox(f"Extracted {len(current)} components.\nPosting to {SERVER}", "Lolit")

            try:
                post_kicad_diff(SERVER, REPO, os.path.basename(path), current)
            except Exception as e:
                wx.MessageBox(f"Failed to post diff: {e}", "Lolit")

    LolitActionPlugin().register()


_register_plugin()
