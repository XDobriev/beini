import json
import base64
import os
import sys

sys.stdout.reconfigure(encoding='utf-8')

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), '..'))
dest_dir = os.path.join(REPO_ROOT, "assets", "images", "instagram")
os.makedirs(dest_dir, exist_ok=True)

source_file = sys.argv[1] if len(sys.argv) > 1 else os.path.join(REPO_ROOT, "instagram_images.json")

if not os.path.exists(source_file):
    print(f"Файл {source_file} не найден.")
    print("Использование: python extract_images.py [путь_к_файлу.json]")
    sys.exit(0)

with open(source_file, "r", encoding="utf-8") as f:
    data = json.load(f)

print(f"Всего объектов в JSON: {len(data)}")

manifest = []
for i, item in enumerate(data):
    b64_data = item.get("base64", "")
    if not b64_data or "," not in b64_data:
        continue
    
    header, encoded = b64_data.split(",", 1)
    ext = "jpg"
    if "png" in header:
        ext = "png"
    elif "webp" in header:
        ext = "webp"
    
    alt = item.get("alt", "").lower()
    src = item.get("src", "")
    
    if "maz_burger" in src or "maz_burger" in alt:
        continue
        
    prefix = "dish"
    if "профиля" in alt:
        prefix = "logo_avatar"
    elif "актуальном" in alt:
        prefix = "highlight"
    elif "video" in alt or "клип" in alt:
        prefix = "reel"
    elif "паста" in alt or "салат" in alt:
        prefix = "dish_salad"
    elif "коктейль" in alt or "сок" in alt or "капучино" in alt:
        prefix = "drink"
    elif "мясо" in alt or "сувлаки" in alt or "тушеное" in alt or "тарелка" in alt:
        prefix = "dish_meat"
        
    filename = f"{prefix}_{i:02d}.{ext}"
    filepath = os.path.join(dest_dir, filename)
    
    img_bytes = base64.b64decode(encoded)
    with open(filepath, "wb") as img_file:
        img_file.write(img_bytes)
        
    print(f"Сохранено: {filename} ({len(img_bytes)} байт) - Alt: {item.get('alt', '')[:50]}")
    manifest.append({
        "filename": filename,
        "path": f"assets/images/instagram/{filename}",
        "alt": item.get("alt", ""),
        "size": len(img_bytes)
    })

manifest_file = os.path.join(dest_dir, "manifest.json")
with open(manifest_file, "w", encoding="utf-8") as f:
    json.dump(manifest, f, ensure_ascii=False, indent=2)

print(f"Успешно обработано и сохранено {len(manifest)} ассетов Instagram!")

