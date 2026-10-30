import urllib.request
import urllib.error
import json
import sys
import os

sys.stdout.reconfigure(encoding='utf-8')

def find_config_path():
    env_cfg = os.environ.get('IIKO_CONFIG_PATH')
    if env_cfg and os.path.exists(env_cfg):
        return env_cfg

    candidates = [
        os.path.join(os.path.dirname(__file__), '..', 'iiko_config.json'),
        os.path.join(os.path.dirname(__file__), 'iiko_config.json'),
        os.path.abspath('iiko_config.json')
    ]
    for p in candidates:
        if os.path.exists(p):
            return os.path.abspath(p)
    return os.path.abspath(candidates[0])

config_path = find_config_path()
if not os.path.exists(config_path):
    print(f"Ошибка: конфигурационный файл не найден: {config_path}")
    sys.exit(1)

with open(config_path, 'r', encoding='utf-8') as f:
    cfg = json.load(f)

payload_v2 = {
    'apiLogin': cfg['apiKey'],
    'appId': cfg['appId'],
    'clientSecret': cfg['clientSecret']
}
req2 = urllib.request.Request(
    f"{cfg['apiBaseUrl']}/api/v2/access_token",
    data=json.dumps(payload_v2).encode('utf-8'),
    headers={'Content-Type': 'application/json'}
)
try:
    with urllib.request.urlopen(req2) as resp2:
        token = json.loads(resp2.read().decode())['token']
        print("✓ Токен получен успешно!")
except urllib.error.HTTPError as e:
    err_body = e.read().decode('utf-8')
    print(f"Ошибка получения токена ({e.code}): {err_body}")
    sys.exit(1)

def call(endpoint, data=None):
    req = urllib.request.Request(
        f"{cfg['apiBaseUrl']}{endpoint}",
        data=json.dumps(data).encode('utf-8') if data is not None else None,
        headers={
            'Content-Type': 'application/json',
            'Authorization': f'Bearer {token}'
        }
    )
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        print(f"Error {endpoint}: {e.code} - {e.read().decode()}")
        return None

# 1. Организации
print("\n--- 1. Организации ---")
orgs = call('/api/1/organizations')
print("Orgs:", orgs)

# 2. Терминальные группы (кассы)
print("\n--- 2. Терминальные группы ---")
if orgs and 'organizations' in orgs:
    org_ids = [o['id'] for o in orgs['organizations']]
    print("Org IDs:", org_ids)
    tg = call('/api/1/terminal_groups', {'organizationIds': org_ids})
    print("Terminal groups:", tg)

# 3. Внешние меню (External Menus)
print("\n--- 3. Внешние меню (v2) ---")
menus = call('/api/2/menu')
print("Menus v2:", menus)

# 4. Стоп-лист (Out of stock items)
print("\n--- 4. Стоп-лист ---")
stop_list = call('/api/1/stop_lists', {'organizationIds': [cfg['organizationId']]})
print("Stop list:", stop_list)
