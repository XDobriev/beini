import re
import urllib.request
import os
import json
import sys

sys.stdout.reconfigure(encoding='utf-8')

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
dest_dir = os.path.join(REPO_ROOT, "assets", "images", "instagram")
os.makedirs(dest_dir, exist_ok=True)

# Скрипт принимает путь к файлу с сырым дампом ответов Instagram
source_dump = sys.argv[1] if len(sys.argv) > 1 else None
if not source_dump or not os.path.exists(source_dump):
    print("Использование: python download_insta.py <путь_к_дампу_со_src_и_alt>")
    print("Пример: python download_insta.py dump_instagram.txt")
    sys.exit(0)

with open(source_dump, "r", encoding="utf-8") as f:
    text = f.read()

# Шаблон поиска src и alt
pattern = r'\"src\":\s*\"(https://[^\"]+)\".*?\"alt\":\s*\"([^\"]*)\"'
matches = re.findall(pattern, text, re.DOTALL)
print(f"Найдено совпадений: {len(matches)}")

headers = {
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
}

saved = []
for idx, (src, alt) in enumerate(matches):
    src = src.replace("\\u0026", "&")
    if "maz_burger" in src:
        continue
    
    alt_lower = alt.lower()
    prefix = "post"
    if "профиля" in alt_lower:
        prefix = "logo_avatar"
    elif "актуальном" in alt_lower:
        prefix = "story_highlight"
    elif "video" in alt_lower or "reel" in alt_lower:
        prefix = "reel_cover"
    
    filename = f"{prefix}_{idx:02d}.jpg"
    filepath = os.path.join(dest_dir, filename)
    
    try:
        req = urllib.request.Request(src, headers=headers)
        with urllib.request.urlopen(req) as resp:
            content = resp.read()
            with open(filepath, "wb") as out:
                out.write(content)
            print(f"Скачано {filename} ({len(content)} байт) - {alt[:40]}")
            saved.append({"filename": filename, "alt": alt, "size": len(content)})
    except Exception as e:
        print(f"Ошибка {filename}: {e}")

manifest_path = os.path.join(dest_dir, "manifest.json")
with open(manifest_path, "w", encoding="utf-8") as f:
    json.dump(saved, f, ensure_ascii=False, indent=2)

print(f"Готово! Сохранено {len(saved)} изображений в {dest_dir}")

