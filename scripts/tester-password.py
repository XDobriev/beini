#!/usr/bin/env python3
"""Create a private password verifier without echo, CLI secrets or terminal output.

python scripts/tester-password.py --output <private file outside checkout>
Add --env for a systemd EnvironmentFile fragment. Never reuse a mock password.
"""
import argparse
import getpass
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--env", action="store_true")
    args = parser.parse_args()
    if not sys.stdin.isatty():
        parser.error("Запустите в интерактивном терминале со скрытым вводом пароля")
    output = args.output.resolve()
    static = Path(os.environ.get("STATIC_DIR", ROOT)).resolve()
    if output.is_relative_to(ROOT) or output.is_relative_to(static) or not output.parent.is_dir() or output.exists():
        parser.error("Use a new file in an existing private directory outside the checkout")
    password = getpass.getpass("Новый пароль (минимум 12 байт): ")
    if password != getpass.getpass("Повторите пароль: "):
        parser.error("Пароли не совпадают")
    result = subprocess.run(["go", "run", "./cmd/password-hash"], input=password.encode(),
                            cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    password = ""
    if result.returncode:
        parser.error("Хеш не создан: проверьте Go 1.24+ и длину пароля 12–1024 байт")
    encoded = result.stdout.decode("ascii").strip()
    if not encoded.startswith("pbkdf2-sha256$600000$"):
        parser.error("Неожиданный формат хеша")
    content = f"BEINI_TESTER_PASSWORD_HASH='{encoded}'\n" if args.env else encoded + "\n"
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as target:
        target.write(content)
    print("Файл создан. На Windows дополнительно ограничьте ACL владельцем и сервисом.")


if __name__ == "__main__":
    main()
