import urllib.request
import re
import os
import sys

sys.stdout.reconfigure(encoding='utf-8')

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
dest_dir = os.path.join(REPO_ROOT, "assets", "images", "taplink")
os.makedirs(dest_dir, exist_ok=True)

url = "https://taplink.cc/beini.rest"
req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"})
try:
    html = urllib.request.urlopen(req).read().decode("utf-8")
except Exception as e:
    print(f"Ошибка загрузки Taplink страницы: {e}")
    sys.exit(1)

# Поиск паттернов вида "0/b/c/3/67139355.jpg"
filenames = re.findall(r"([0-9a-f]/[0-9a-f]/[0-9a-f]/[0-9a-f]/\d+\.(?:jpg|png|jpeg))", html)
print(f"Найдено ссылок на медиа: {len(filenames)}")
for f in set(filenames):
    img_url = f"https://br.x.taplink.st/p/{f}"
    dest = os.path.join(dest_dir, os.path.basename(f))
    print(f"Скачивание {img_url} -> {dest}")
    try:
        urllib.request.urlretrieve(img_url, dest)
        print("✓ Успешно:", os.path.getsize(dest), "байт")
    except Exception as e:
        print(f"Ошибка при загрузке: {e}")

