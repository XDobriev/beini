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
    print(f"Ошибка: файл конфигурации не найден по пути: {config_path}")
    sys.exit(1)

with open(config_path, 'r', encoding='utf-8') as f:
    cfg = json.load(f)

# 1. Получение токена (пробуем v1, при отказе v2)
token_payload = {
    'apiLogin': cfg['apiKey']
}
req = urllib.request.Request(
    f"{cfg['apiBaseUrl']}/api/1/access_token",
    data=json.dumps(token_payload).encode('utf-8'),
    headers={'Content-Type': 'application/json'}
)

token = None
try:
    with urllib.request.urlopen(req) as resp:
        token = json.loads(resp.read().decode())['token']
        print('Авторизация через /api/1/access_token успешна!')
except Exception as e:
    print('Авторизация v1 отклонена, пробуем v2 (appId + clientSecret):', e)
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
    with urllib.request.urlopen(req2) as resp2:
        token = json.loads(resp2.read().decode())['token']
        print('Авторизация через /api/v2/access_token успешна!')

# 2. Получение номенклатуры
nom_payload = {'organizationId': cfg['organizationId']}
nom_req = urllib.request.Request(
    f"{cfg['apiBaseUrl']}/api/1/nomenclature",
    data=json.dumps(nom_payload).encode('utf-8'),
    headers={
        'Content-Type': 'application/json',
        'Authorization': f'Bearer {token}'
    }
)

try:
    with urllib.request.urlopen(nom_req) as resp:
        data = json.loads(resp.read().decode())
        print('Номенклатура успешно получена!')
        out_path = os.path.join(os.path.dirname(config_path), 'iiko_nomenclature.json')
        with open(out_path, 'w', encoding='utf-8') as out:
            json.dump(data, out, ensure_ascii=False, indent=2)
        print(f"Сохранено в: {out_path}")
        print('Количество категорий (groups):', len(data.get('groups', [])))
        print('Количество блюд (products):', len(data.get('products', [])))
except Exception as e:
    print('Ошибка загрузки номенклатуры:', e)

