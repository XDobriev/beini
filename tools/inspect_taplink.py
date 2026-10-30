import re
import os
import sys

sys.stdout.reconfigure(encoding='utf-8')

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
source_dump = sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, "taplink_dump.html")

if not os.path.exists(source_dump):
    print(f"Файл {source_dump} не найден.")
    print("Использование: python inspect_taplink.py [путь_к_дампу.html]")
    sys.exit(0)

with open(source_dump, "r", encoding="utf-8", errors="ignore") as f:
    text = f.read()

# Поиск всех тегов картинок и URL
imgs = re.findall(r'src=[\'"]([^\'"]+)[\'"]', text)
print(f"Найдено IMG SRC: {len(imgs)}")
for s in imgs[:10]:
    print("  ", s)

# Поиск имен файлов
files = re.findall(r'filename["\':\s]+([^"\'\}\,]+)', text)
print(f"\nНайдено FILENAMES: {len(files)}")
for f in files[:5]:
    idx = text.find(f)
    print("Контекст вокруг имени файла:", text[max(0, idx-100):min(len(text), idx+100)])

