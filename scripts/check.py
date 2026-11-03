#!/usr/bin/env python3
"""Offline project checks. Requires Go, Node.js and Python; no POS credentials."""
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main():
    missing = [name for name in ("go", "node") if shutil.which(name) is None]
    if sys.version_info < (3, 10):
        missing.append("Python 3.10+")
    if missing:
        print("Missing tools: " + ", ".join(missing), file=sys.stderr)
        return 1

    # Keep build products outside the checkout and clean them even after failure.
    with tempfile.TemporaryDirectory(prefix="beini-check-") as build_dir:
        binary = str(Path(build_dir) / "beini-server")
        steps = [
            ("Go tests", ["go", "test", "./..."]),
            ("Go vet", ["go", "vet", "./..."]),
            ("Server build", ["go", "build", "-o", binary, "./cmd/server"]),
            ("App syntax", ["node", "--check", "js/app.js"]),
            ("Access client syntax", ["node", "--check", "js/access.js"]),
            ("Tester syntax", ["node", "--check", "js/tester.js"]),
            ("Access checkout", ["node", "scripts/check-access.cjs"]),
            ("Access HTTP client", ["node", "scripts/check-access-client.cjs"]),
            ("Mock access and checkout", [sys.executable, "-B", "-m", "unittest", "discover", "-s", "scripts", "-p", "test_mock_ui.py"]),
            ("Checkout persistence", ["node", "scripts/check-checkout.cjs"]),
            ("Menu syntax", ["node", "--check", "js/menu-data.js"]),
            ("Menu consistency", ["node", "scripts/check-menu.cjs"]),
            ("Generated menu", [sys.executable, "-B", "tools/export_menu.py", "--check"]),
            ("Menu UI contract", ["node", "scripts/check-menu-ui.cjs"]),
            ("Importer tests", [sys.executable, "-B", "-m", "unittest", "discover",
                                "-s", "tools", "-p", "test_import_menu.py"]),
            ("V3 audit tests", [sys.executable, "-B", "-m", "unittest", "discover",
                               "-s", "tools", "-p", "test_audit_menu_v3.py"]),
            ("Menu audit", [sys.executable, "-B", "tools/import_menu.py", "--check"]),
        ]
        failed = []
        for label, command in steps:
            print("\n" + label, flush=True)
            result = subprocess.run(command, cwd=ROOT, check=False)
            if result.returncode:
                failed.append(label)
        if failed:
            print("\nFAILED: " + ", ".join(failed), file=sys.stderr)
            return 1
    print("\nAll checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
