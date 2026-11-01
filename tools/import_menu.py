#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
tools/import_menu.py — Автономный импортёр и аудитор меню ресторана BEINI.

Безопасный процесс синхронизации каталога сайта с номенклатурой iikoCloud:
- Не зависит от runtime-запросов сайта к iiko (сайт работает автономно по Zero-Bloat стандарту).
- Не сохраняет секреты и API-ключи в исходный код (конфиг iiko_config.json или ENV).
- НЕ выполняет сетевых запросов и импорта без явной команды пользователя (--fetch).
- Поддерживает безопасный аудит (--check, --dry-run) и сверку с локальным дампом (--from-file).
- Проверяет 4 обязательных правила для каждого отображаемого блюда:
  1) iiko ID (валидный UUID v4);
  2) цена (число > 0);
  3) категория (привязка к допустимой категории сайта);
  4) картинка (физически существующий файл на диске).
- Формирует подробный отчет: добавленные, скрытые, недоступные и позиции без фото/категории.
"""

import argparse
import copy
import json
import math
import os
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

# Установка UTF-8 для вывода в консоль Windows
if hasattr(sys.stdout, 'reconfigure'):
    sys.stdout.reconfigure(encoding='utf-8')

# Регулярное выражение для валидации UUID v4
UUID_REGEX = re.compile(
    r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    re.IGNORECASE
)

# Допустимые категории сайта и их порядок
ALLOWED_CATEGORIES = [
    'eastern',
    'grill',
    'soups',
    'salads',
    'sushi',
    'pizza',
    'desserts',
    'drinks'
]

CATEGORY_NAMES = {
    'eastern': 'Восточная & Национальная',
    'grill': 'Мангал & Стейки',
    'soups': 'Супы & Том-Ям',
    'salads': 'Салаты & Закуски',
    'sushi': 'Суши & Роллы',
    'pizza': 'Пицца & Выпечка',
    'desserts': 'Десерты',
    'drinks': 'Айс-кофе & Бар'
}

CATEGORY_ICONS = {
    'eastern': 'utensils',
    'grill': 'flame',
    'soups': 'soup',
    'salads': 'salad',
    'sushi': 'fish',
    'pizza': 'pizza',
    'desserts': 'cake',
    'drinks': 'coffee'
}


def get_repo_root() -> Path:
    """Возвращает абсолютный путь к корню репозитория BEINI."""
    return Path(__file__).resolve().parent.parent


def load_config() -> Dict[str, Any]:
    """
    Безопасная загрузка конфигурации iiko.
    Приоритеты:
    1. Переменная окружения IIKO_CONFIG_PATH
    2. Файл iiko_config.json в корне или в tools/
    3. Отдельные переменные окружения IIKO_API_KEY, IIKO_APP_ID и др.
    """
    root = get_repo_root()
    env_cfg_path = os.environ.get('IIKO_CONFIG_PATH')
    candidates = []
    if env_cfg_path:
        candidates.append(Path(env_cfg_path))
    candidates.extend([
        root / 'iiko_config.json',
        Path(__file__).resolve().parent / 'iiko_config.json',
    ])

    for p in candidates:
        if p.exists():
            try:
                with open(p, 'r', encoding='utf-8') as f:
                    cfg = json.load(f)
                    return cfg
            except Exception as e:
                print(f"[!] Ошибка чтения файла конфигурации {p}: {e}")

    # Fallback на переменные окружения
    return {
        'apiBaseUrl': os.environ.get('IIKO_BASE_URL', 'https://api-ru.iiko.services'),
        'apiKey': os.environ.get('IIKO_API_KEY', ''),
        'appId': os.environ.get('IIKO_APP_ID', ''),
        'clientSecret': os.environ.get('IIKO_CLIENT_SECRET', ''),
        'organizationId': os.environ.get('IIKO_ORGANIZATION_ID', ''),
        'terminalGroupId': os.environ.get('IIKO_TERMINAL_GROUP_ID', '')
    }


def get_cached_token_path() -> Path:
    """Путь к файлу кэша JWT-токена в корне проекта."""
    return get_repo_root() / '.iiko_token_cache.json'


def fetch_token(config: Dict[str, Any]) -> str:
    """
    Получение JWT-токена iikoCloud с безопасным кэшированием на 50 минут.
    """
    cache_file = get_cached_token_path()
    if cache_file.exists():
        try:
            with open(cache_file, 'r', encoding='utf-8') as cf:
                cdata = json.load(cf)
                if time.time() - cdata.get('ts', 0) < 3000:
                    return cdata['token']
        except Exception:
            pass

    api_key = config.get('apiKey', '').strip()
    app_id = config.get('appId', '').strip()
    client_secret = config.get('clientSecret', '').strip()
    base_url = config.get('apiBaseUrl', 'https://api-ru.iiko.services').rstrip('/')

    if not api_key:
        raise ValueError(
            "Не задан apiKey iiko. Укажите его в iiko_config.json или через переменную IIKO_API_KEY."
        )

    # Авторизация iiko API v2 (рекомендуемый протокол iikoCloud)
    payload_v2 = {
        'apiKey': api_key,
        'appId': app_id,
        'clientSecret': client_secret
    }
    req = urllib.request.Request(
        f"{base_url}/api/v2/access_token",
        data=json.dumps(payload_v2).encode('utf-8'),
        headers={'Content-Type': 'application/json'}
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            token = json.loads(resp.read().decode())['token']
            with open(cache_file, 'w', encoding='utf-8') as cf:
                json.dump({'token': token, 'ts': time.time()}, cf)
            return token
    except urllib.error.HTTPError as e:
        # Never include API response bodies: they can contain credentials.
        e.read()
        if e.code == 423:
            raise RuntimeError(
                "HTTP 423 Locked: В iikoWeb истекла лицензия Cloud API для apiLogin ресторана.\n"
                "Продлите подписку/лицензию в iikoWeb или используйте автономный режим (--from-file <файл>)."
            ) from e
        elif e.code == 401:
            raise RuntimeError(
                f"HTTP 401 Unauthorized: Проверьте правильность apiLogin, appId и clientSecret в iiko_config.json."
            ) from e
        raise RuntimeError(f"HTTP {e.code} при авторизации в iikoCloud") from e


def fetch_live_nomenclature(config: Dict[str, Any]) -> Dict[str, Any]:
    """
    Запрос свежей номенклатуры из iikoCloud API (/api/1/nomenclature).
    Выполняется ТОЛЬКО по явной команде пользователя (--fetch).
    """
    org_id = config.get('organizationId', '').strip()
    if not org_id:
        raise ValueError("В конфигурации отсутствует organizationId ресторана BEINI.")

    token = fetch_token(config)
    base_url = config.get('apiBaseUrl', 'https://api-ru.iiko.services').rstrip('/')

    payload = {'organizationId': org_id}
    req = urllib.request.Request(
        f"{base_url}/api/1/nomenclature",
        data=json.dumps(payload).encode('utf-8'),
        headers={
            'Content-Type': 'application/json',
            'Authorization': f'Bearer {token}'
        }
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode('utf-8'))


def parse_stop_lists(data: Dict[str, Any], org_id: str, terminal_id: str) -> Set[str]:
    """Only an explicit items:[] for the selected terminal means empty."""
    if not org_id or not terminal_id:
        raise ValueError("Нужны organizationId и terminalGroupId для стоп-листа.")
    if not isinstance(data, dict) or any(data.get(k) for k in ('error', 'errorCode', 'errorDescription')):
        raise ValueError("Ошибка или поврежденный ответ стоп-листа.")
    groups = data.get('terminalGroupStopLists')
    if not isinstance(groups, list):
        raise ValueError("Отсутствует terminalGroupStopLists.")
    organizations = [g for g in groups if isinstance(g, dict) and g.get('organizationId') == org_id]
    if len(organizations) != 1 or not isinstance(organizations[0].get('items'), list):
        raise ValueError("Стоп-лист организации отсутствует или неоднозначен.")
    terminals = [t for t in organizations[0]['items']
                 if isinstance(t, dict) and t.get('terminalGroupId') == terminal_id]
    if len(terminals) != 1 or not isinstance(terminals[0].get('items'), list):
        raise ValueError("Стоп-лист выбранной терминальной группы отсутствует или неоднозначен.")
    stopped, seen = set(), set()
    for item in terminals[0]['items']:
        if not isinstance(item, dict):
            raise ValueError("Поврежденная позиция стоп-листа.")
        pid, balance = item.get('productId'), item.get('balance')
        if (not isinstance(pid, str) or not pid or pid in seen
                or isinstance(balance, bool) or not isinstance(balance, (int, float))
                or not math.isfinite(balance) or item.get('sizeId') is not None):
            raise ValueError("Поврежденная позиция стоп-листа.")
        seen.add(pid)
        if balance <= 0:
            stopped.add(pid)
    return stopped


def fetch_live_stop_lists(config: Dict[str, Any]) -> Set[str]:
    """Failure propagates; it must never become an empty stop-list."""
    org_id = config.get('organizationId', '').strip()
    terminal_id = config.get('terminalGroupId', '').strip()
    if not org_id or not terminal_id:
        raise ValueError("Нужны organizationId и terminalGroupId для стоп-листа.")
    token = fetch_token(config)
    base_url = config.get('apiBaseUrl', 'https://api-ru.iiko.services').rstrip('/')
    req = urllib.request.Request(
        f"{base_url}/api/1/stop_lists",
        data=json.dumps({'organizationIds': [org_id], 'terminalGroupsIds': [terminal_id],
                         'returnSize': False}).encode('utf-8'),
        headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {token}'}
    )
    with urllib.request.urlopen(req, timeout=15) as resp:
        return parse_stop_lists(json.loads(resp.read().decode('utf-8')), org_id, terminal_id)


def product_price(product: Dict[str, Any]) -> float:
    """Do not silently choose a size or publish unsupported modifier combinations."""
    prices = product.get('sizePrices')
    if not isinstance(prices, list):
        raise ValueError("Поврежденные цены номенклатуры.")
    for price in prices:
        if not isinstance(price, dict) or not isinstance(price.get('price'), dict):
            raise ValueError("Поврежденные цены номенклатуры.")
        if not isinstance(price['price'].get('isIncludedInMenu'), bool):
            raise ValueError("Отсутствует признак включения цены в меню.")
        value = price['price'].get('currentPrice')
        if isinstance(value, bool) or not isinstance(value, (float, int)) or not math.isfinite(value) or value < 0:
            raise ValueError("Некорректная цена номенклатуры.")
    if (product.get('isDeleted') or str(product.get('type', '')).lower() not in ('dish', 'good')
            or product.get('useBalanceForSell') is not False
            or product.get('canSetOpenPrice') is not False
            or product.get('orderItemType', 'Product') != 'Product'
            or product.get('isSoldByWeight') or product.get('modifiers') or product.get('groupModifiers')
            or len(prices) != 1 or prices[0].get('sizeId')
            or product.get('isIncludedInMenu') is False
            or prices[0]['price'].get('isIncludedInMenu') is not True):
        return 0
    return prices[0]['price']['currentPrice']


def validate_nomenclature(data: Dict[str, Any]) -> None:
    if not isinstance(data, dict) or not isinstance(data.get('products'), list) or not data['products']:
        raise ValueError("Ожидается полная непустая номенклатура.")
    seen = set()
    for p in data['products']:
        if (not isinstance(p, dict) or not isinstance(p.get('id'), str) or not p['id'] or p['id'] in seen
                or not isinstance(p.get('isDeleted'), bool)):
            raise ValueError("Некорректный идентификатор номенклатуры.")
        seen.add(p['id'])
        if not p['isDeleted']:
            if not p.get('name') or not p.get('type') or not p.get('sizePrices'):
                raise ValueError("Неполная номенклатура.")
            product_price(p)


def apply_reconciliation(local: Dict[str, Any], nomenclature: Dict[str, Any],
                         stopped_ids: Optional[Set[str]]) -> Dict[str, Any]:
    validate_nomenclature(nomenclature)
    result = copy.deepcopy(local)
    products = {p['id']: p for p in nomenclature['products']}
    for item in result.get('items', []):
        prod = products.get(item.get('iikoId'))
        price = product_price(prod) if prod and not prod['isDeleted'] else 0
        if price > 0:
            item['price'] = price
        # Preserve editorial exclusions. Restoring stopped items belongs to the
        # server's separate sellable flag, not to a one-off destructive import.
        item['available'] = (item.get('available', True) and price > 0
                             and (stopped_ids is None or item['iikoId'] not in stopped_ids))
    result['lastUpdated'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    return result


def load_local_catalog(path: Optional[Path] = None) -> Dict[str, Any]:
    """Загружает локальный каталог блюд из data/menu_catalog.json."""
    if path is None:
        path = get_repo_root() / 'data' / 'menu_catalog.json'

    if not path.exists():
        # Если JSON-каталог еще не существует, инициализируем пустой структурой
        return {
            'version': '1.0.0',
            'restaurant': 'BEINI',
            'city': 'Назрань',
            'lastUpdated': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
            'categories': [],
            'items': []
        }

    with open(path, 'r', encoding='utf-8') as f:
        return json.load(f)


def validate_dish(dish: Dict[str, Any], root: Path) -> List[str]:
    """
    Проверяет 4 обязательных правила для отображаемого блюда:
    1) iiko ID (наличие и валидный UUID v4);
    2) цена (число > 0);
    3) категория (привязана к одной из разрешённых категорий сайта);
    4) картинка (файл физически существует на диске).
    Возвращает список ошибок (пустой, если блюдо валидно).
    """
    errors = []

    # 1. iikoId
    iiko_id = dish.get('iikoId', '')
    if not iiko_id:
        errors.append("Отсутствует обязательный iiko ID")
    elif not UUID_REGEX.match(str(iiko_id)):
        errors.append(f"Некорректный формат iiko ID (ожидается UUID): '{iiko_id}'")

    # 2. Цена
    price = dish.get('price')
    if price is None:
        errors.append("Отсутствует цена")
    elif not isinstance(price, (int, float)):
        errors.append(f"Цена должна быть числом, получено: {type(price).__name__}")
    elif price <= 0:
        errors.append(f"Цена должна быть больше нуля (текущая: {price} ₽)")

    # 3. Категория
    cat = dish.get('category', '')
    if not cat:
        errors.append("Не назначена категория блюда")
    elif cat not in ALLOWED_CATEGORIES:
        errors.append(f"Неизвестная категория '{cat}'. Разрешены: {', '.join(ALLOWED_CATEGORIES)}")

    # 4. Картинка
    img_path = dish.get('image', '')
    if not img_path:
        errors.append("Отсутствует ссылка на изображение (поле image)")
    else:
        # Проверяем существование файла на диске относительно корня репозитория
        full_img = root / img_path.replace('/', os.sep)
        if not full_img.is_file():
            errors.append(f"Файл изображения не найден на диске: '{img_path}'")

    return errors


def audit_catalog(catalog: Dict[str, Any], root: Path) -> Dict[str, Any]:
    """
    Проводит строгий локальный аудит каталога без сетевых запросов.
    """
    items = catalog.get('items', [])
    valid_items = []
    invalid_items = []

    for item in items:
        errs = validate_dish(item, root)
        if errs:
            invalid_items.append({'item': item, 'errors': errs})
        else:
            valid_items.append(item)

    return {
        'total': len(items),
        'validCount': len(valid_items),
        'invalidCount': len(invalid_items),
        'validItems': valid_items,
        'invalidItems': invalid_items
    }


def reconcile_menu(
    local_catalog: Dict[str, Any],
    nomenclature_data: Dict[str, Any],
    stopped_ids: Optional[Set[str]],
    root: Path
) -> Dict[str, Any]:
    """
    Сверяет локальный каталог сайта с номенклатурой iiko.
    Формирует отчёт:
    - added: новые активные позиции в iiko, которых нет в каталоге
    - hidden: позиции из каталога, помеченные как удаленные в iiko или исчезнувшие
    - unavailable: позиции в стоп-листе или с нулевой ценой в iiko
    - missing_media_or_cat: позиции каталога без существующего фото или без категории
    """
    validate_nomenclature(nomenclature_data)
    local_items = local_catalog.get('items', [])
    local_by_iiko = {item.get('iikoId'): item for item in local_items if item.get('iikoId')}

    iiko_products = nomenclature_data.get('products', [])
    iiko_groups = {g.get('id'): g.get('name') for g in nomenclature_data.get('groups', [])}
    iiko_categories = {c.get('id'): c.get('name') for c in nomenclature_data.get('productCategories', [])}

    iiko_by_id: Dict[str, Dict[str, Any]] = {}
    for prod in iiko_products:
        pid = prod.get('id')
        if pid:
            iiko_by_id[pid] = prod

    added: List[Dict[str, Any]] = []
    hidden: List[Dict[str, Any]] = []
    unavailable: List[Dict[str, Any]] = []
    missing_media_or_cat: List[Dict[str, Any]] = []
    price_changed: List[Dict[str, Any]] = []

    # 1. Анализируем блюда iiko -> ищем добавленные
    for pid, prod in iiko_by_id.items():
        is_deleted = prod.get('isDeleted', False)
        prod_type = prod.get('type', '')

        # Нас интересуют блюда для продажи (Dish, Good, Modifier при необходимости)
        if is_deleted:
            continue

        # Извлекаем цену
        price = product_price(prod) if not is_deleted else 0

        if pid not in local_by_iiko:
            # Новая позиция в iiko
            group_name = iiko_groups.get(prod.get('parentGroup'), 'Без группы iiko')
            cat_name = iiko_categories.get(prod.get('productCategoryId'), '')
            added.append({
                'iikoId': pid,
                'name': prod.get('name', 'Без названия'),
                'price': price,
                'weight': prod.get('weight', 0),
                'group': group_name,
                'category': cat_name,
                'type': prod_type
            })

    # 2. Анализируем локальные блюда сайта
    for item in local_items:
        iid = item.get('iikoId')
        name = item.get('name', 'Без названия')
        iiko_prod = iiko_by_id.get(iid) if iid else None

        # Проверка удаления в iiko
        if not iiko_prod or iiko_prod.get('isDeleted', False):
            hidden.append({
                'item': item,
                'reason': 'Удалено в iiko' if (iiko_prod and iiko_prod.get('isDeleted')) else 'Отсутствует в номенклатуре iiko'
            })
            continue

        # Проверка доступности (стоп-лист или 0 руб.)
        is_stopped = stopped_ids is not None and iid in stopped_ids
        iiko_price = product_price(iiko_prod)

        if is_stopped:
            unavailable.append({
                'item': item,
                'reason': 'В стоп-листе кассового терминала iikoFront'
            })
        elif iiko_price <= 0 and item.get('price', 0) > 0:
            unavailable.append({
                'item': item,
                'reason': 'В iiko указана нулевая цена (блюдо не готово к продаже)'
            })

        # Проверка изменения цены
        if iiko_price > 0 and item.get('price') != iiko_price:
            price_changed.append({
                'item': item,
                'oldPrice': item.get('price'),
                'newPrice': iiko_price
            })

        # Проверка фото и категории
        val_errs = validate_dish(item, root)
        photo_or_cat_errs = [e for e in val_errs if 'изображен' in e or 'категори' in e]
        if photo_or_cat_errs:
            missing_media_or_cat.append({
                'item': item,
                'errors': photo_or_cat_errs
            })

    return {
        'stopListKnown': stopped_ids is not None,
        'totalLocal': len(local_items),
        'totalIiko': len(iiko_products),
        'added': added,
        'hidden': hidden,
        'unavailable': unavailable,
        'missingMediaOrCat': missing_media_or_cat,
        'priceChanged': price_changed
    }


def format_report(report_data: Dict[str, Any], dry_run: bool = True) -> str:
    """Форматирует текстовый отчёт аудита и сверки меню."""
    lines = []
    lines.append("=" * 72)
    lines.append("       ОТЧЁТ ОБНОВЛЕНИЯ И АУДИТА МЕНЮ BEINI (iikoCloud Integration)")
    lines.append("=" * 72)
    lines.append(f"Режим работы: {'[ПРОВЕРКА / DRY-RUN] Файлы не изменялись' if dry_run else '[ПРИМЕНЕНИЕ / APPLIED] Каталог обновлен'}")
    lines.append(f"Время аудита: {time.strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append("-" * 72)

    # 1. Добавленные позиции
    added = report_data.get('added', [])
    lines.append(f"\n[+] ДОБАВЛЕННЫЕ В IIKO ПОЗИЦИИ (НОВЫЕ БЛЮДА): {len(added)}")
    if added:
        lines.append("    (Эти блюда есть в базе iiko, но пока не добавлены на сайт)")
        for a in added[:25]:
            lines.append(f"    • {a['name']} — {a['price']} ₽ | Группа: {a['group']} | iikoId: {a['iikoId']}")
        if len(added) > 25:
            lines.append(f"    ... и ещё {len(added) - 25} позиций.")
    else:
        lines.append("    ✓ Новых позиций в iiko не обнаружено.")

    # 2. Скрытые / удаленные позиции
    hidden = report_data.get('hidden', [])
    lines.append(f"\n[-] СКРЫТЫЕ / УДАЛЁННЫЕ ПОЗИЦИИ: {len(hidden)}")
    if hidden:
        lines.append("    (Блюда на сайте, которые помечены удаленными в iiko или отсутствуют)")
        for h in hidden:
            it = h['item']
            lines.append(f"    • {it.get('name')} (iikoId: {it.get('iikoId')}) — {h['reason']}")
    else:
        lines.append("    ✓ Нет скрытых или удаленных позиций.")

    # 3. Недоступные позиции
    unavail = report_data.get('unavailable', [])
    lines.append(f"\n[!] НЕДОСТУПНЫЕ ПОЗИЦИИ (СТОП-ЛИСТ / НУЛЕВАЯ ЦЕНА): {len(unavail)}")
    if unavail:
        for u in unavail:
            it = u['item']
            lines.append(f"    • {it.get('name')} (iikoId: {it.get('iikoId')}) — {u['reason']}")
    elif report_data.get("stopListKnown", False):
        lines.append("    ✓ Недоступных позиций не обнаружено.")
    else:
        lines.append("    ? Стоп-лист неизвестен; доступность не подтверждена.")

    # 4. Позиции без фото или категории
    missing = report_data.get('missingMediaOrCat', [])
    lines.append(f"\n[?] ПОЗИЦИИ БЕЗ ФОТО ИЛИ БЕЗ КАТЕГОРИИ: {len(missing)}")
    if missing:
        for m in missing:
            it = m['item']
            err_str = "; ".join(m['errors'])
            lines.append(f"    • {it.get('name')} (id: {it.get('id')}) -> {err_str}")
    else:
        lines.append("    ✓ Каждое блюдо имеет привязанную категорию и существующий файл фото.")

    # 5. Изменения цен
    price_ch = report_data.get('priceChanged', [])
    if price_ch:
        lines.append(f"\n[~] ИЗМЕНЕНИЯ ЦЕН В IIKO: {len(price_ch)}")
        for p in price_ch:
            it = p['item']
            lines.append(f"    • {it.get('name')}: {p['oldPrice']} ₽ -> {p['newPrice']} ₽ (в iiko)")

    lines.append("\n" + "=" * 72)
    lines.append(f"Сводка: В каталоге сайта: {report_data.get('totalLocal', 0)} блюд | В iiko: {report_data.get('totalIiko', 0)}")
    lines.append("=" * 72)
    return "\n".join(lines)


def export_js_menu_data(catalog: Dict[str, Any], js_path: Path) -> None:
    """
    Экспортирует проверенный каталог в js/menu-data.js для автономной работы сайта.
    Гарантирует 100% совместимость с Vanilla JS архитектурой BEINI.
    """
    items = catalog.get('items', [])
    # Группируем блюда по категориям с сохранением порядка
    grouped: Dict[str, List[Dict[str, Any]]] = {c: [] for c in ALLOWED_CATEGORIES}

    for item in items:
        cat = item.get('category')
        if cat in grouped and item.get('available', True):
            # Экспортируем чистый объект блюда
            grouped[cat].append({
                'id': item.get('id'),
                'iikoId': item.get('iikoId'),
                'name': item.get('name'),
                'description': item.get('description', ''),
                'price': item.get('price'),
                'weight': item.get('weight', ''),
                'image': item.get('image', ''),
                'badge': item.get('badge', ''),
                'isHalal': item.get('isHalal', True)
            })

    # Собираем MENU_DATA
    menu_sections = []
    for cat in ALLOWED_CATEGORIES:
        cat_items = grouped[cat]
        if not cat_items:
            continue
        menu_sections.append({
            'category': cat,
            'categoryName': CATEGORY_NAMES.get(cat, cat),
            'categoryIcon': CATEGORY_ICONS.get(cat, 'utensils'),
            'items': cat_items
        })

    js_code = (
        "// Актуальное меню ресторана BEINI (Назрань)\n"
        "// Сгенерировано из data/menu_catalog.json с валидацией iiko ID, цен и медиафайлов.\n"
        "// ВНИМАНИЕ: Не редактируйте этот файл вручную во избежание расхождений с кассой.\n"
        f"const MENU_DATA = {json.dumps(menu_sections, ensure_ascii=False, indent=2)};\n"
    )

    with open(js_path, 'w', encoding='utf-8') as f:
        f.write(js_code)


def main():
    parser = argparse.ArgumentParser(
        description="Поддерживаемый импортёр и аудитор меню BEINI из iikoCloud."
    )
    group = parser.add_mutually_exclusive_group()
    group.add_argument(
        '--check', '--validate',
        dest='validate_only',
        action='store_true',
        help="Только проверка локального каталога на наличие iiko ID, цен, категорий и фото (без обращений к сети)."
    )
    group.add_argument(
        '--from-file',
        dest='from_file',
        type=str,
        help="Сверить меню с локальным дампом номенклатуры iiko (без выхода в интернет)."
    )
    group.add_argument(
        '--fetch', '--live',
        dest='live_fetch',
        action='store_true',
        help="ЯВНАЯ КОМАНДА: выполнить сетевой запрос к iikoCloud API для загрузки актуальной номенклатуры."
    )

    parser.add_argument(
        '--apply',
        action='store_true',
        help="Сохранить изменения в локальный каталог data/menu_catalog.json и обновить js/menu-data.js (по умолчанию dry-run)."
    )
    parser.add_argument(
        '--report',
        type=str,
        help="Путь для сохранения отчета об аудите в файл (markdown или txt)."
    )

    parser.add_argument('--stop-list-file', help="Локальный ответ /api/1/stop_lists для --from-file.")
    parser.add_argument('--organization-id', help="Организация для локального стоп-листа.")
    parser.add_argument('--terminal-group-id', help="Терминальная группа для локального стоп-листа.")
    args = parser.parse_args()
    if args.stop_list_file and not args.from_file:
        parser.error('--stop-list-file требует --from-file')
    root = get_repo_root()
    catalog_path = root / 'data' / 'menu_catalog.json'
    js_path = root / 'js' / 'menu-data.js'

    # Если не указан ни один режим, по умолчанию запускаем безопасную локальную проверку
    if not (args.validate_only or args.from_file or args.live_fetch):
        print("[i] Режим по умолчанию: локальная проверка каталога блюд (--check).")
        print("    Для сверки с iiko используйте --fetch (сеть) или --from-file <файл>.")
        args.validate_only = True

    local_catalog = load_local_catalog(catalog_path)

    # 1. Режим: Только валидация локального каталога
    if args.validate_only:
        audit = audit_catalog(local_catalog, root)
        print("=" * 72)
        print("           ВАЛИДАЦИЯ ЛОКАЛЬНОГО КАТАЛОГА БЛЮД BEINI")
        print("=" * 72)
        print(f"Всего блюд в каталоге: {audit['total']}")
        print(f"✓ Прошли проверку (100% валидны): {audit['validCount']}")
        print(f"✗ С ошибками: {audit['invalidCount']}")
        print("-" * 72)

        if audit['invalidItems']:
            print("\n[!] ОБНАРУЖЕНЫ ОШИБКИ В БЛЮДАХ:")
            for entry in audit['invalidItems']:
                item = entry['item']
                print(f"  • {item.get('name', 'Без имени')} (id: {item.get('id', '—')}):")
                for err in entry['errors']:
                    print(f"      - {err}")
            sys.exit(1)
        else:
            print("\n✓ Все блюда имеют корректный iiko ID, цену, категорию и существующее фото на диске.")
            return

    # 2. Режимы с номенклатурой (live или from-file)
    nomenclature_data = None
    stopped_ids: Optional[Set[str]] = None

    if args.from_file:
        file_path = Path(args.from_file)
        if not file_path.exists():
            print(f"[!] Файл с дампом номенклатуры не найден: {file_path}")
            sys.exit(1)
        print(f"[*] Чтение номенклатуры iiko из файла: {file_path}")
        with open(file_path, 'r', encoding='utf-8') as f:
            nomenclature_data = json.load(f)
        if args.stop_list_file:
            with open(args.stop_list_file, 'r', encoding='utf-8') as f:
                stopped_ids = parse_stop_lists(json.load(f), args.organization_id, args.terminal_group_id)

    elif args.live_fetch:
        print("[*] ЯВНАЯ КОМАНДА: Подключение к iikoCloud API ресторана BEINI...")
        config = load_config()
        try:
            nomenclature_data = fetch_live_nomenclature(config)
            print(f"✓ Номенклатура iiko успешно получена ({len(nomenclature_data.get('products', []))} продуктов)!")
            stopped_ids = fetch_live_stop_lists(config)
            if stopped_ids:
                print(f"✓ Получен стоп-лист кассы: {len(stopped_ids)} позиций временно заблокировано.")
        except Exception:
            print("[!] Ошибка подключения к iiko; изменения не применены.")
            sys.exit(1)

    # Выполняем сверку
    reconciled = reconcile_menu(local_catalog, nomenclature_data, stopped_ids, root)
    dry_run = not args.apply
    report_text = format_report(reconciled, dry_run=dry_run)
    print(report_text)

    if args.report:
        report_path = Path(args.report)
        report_path.parent.mkdir(parents=True, exist_ok=True)
        with open(report_path, 'w', encoding='utf-8') as rf:
            rf.write(report_text)
        print(f"\n[✓] Отчет сохранён в файл: {report_path}")

    # Применение изменений, если указан флаг --apply
    if args.apply:
        print("\n[*] Применение изменений к локальному каталогу...")

        local_catalog = apply_reconciliation(local_catalog, nomenclature_data, stopped_ids)

        # Сохраняем data/menu_catalog.json
        catalog_path.parent.mkdir(parents=True, exist_ok=True)
        with open(catalog_path, 'w', encoding='utf-8') as f:
            json.dump(local_catalog, f, ensure_ascii=False, indent=2)
        print(f"✓ Обновлен локальный каталог: {catalog_path}")

        # Экспортируем в js/menu-data.js
        export_js_menu_data(local_catalog, js_path)
        print(f"✓ Синхронизирован файл меню сайта: {js_path}")
        print("✓ Синхронизация успешно завершена!")
    else:
        print("\n[i] Изменения НЕ сохранены на диск (запустите с флагом --apply для сохранения).")


if __name__ == '__main__':
    main()

