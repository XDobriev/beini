import urllib.request
import urllib.error
import json
import os
import sys
import time

sys.stdout.reconfigure(encoding='utf-8')

def find_config_path():
    """
    Поиск файла iiko_config.json:
    1. Переменная окружения IIKO_CONFIG_PATH
    2. В директории tools/
    3. В корне репозитория (../iiko_config.json)
    4. В текущей рабочей директории
    """
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

CONFIG_PATH = find_config_path()
if not os.path.exists(CONFIG_PATH):
    raise FileNotFoundError(
        f"Файл конфигурации не найден! Ожидался по пути: {CONFIG_PATH}\n"
        "Скопируйте iiko_config.example.json в iiko_config.json и заполните учетные данные."
    )

with open(CONFIG_PATH, 'r', encoding='utf-8') as f:
    CONFIG = json.load(f)

CACHE_PATH = os.path.join(os.path.dirname(CONFIG_PATH), '.iiko_token_cache.json')

def get_token():
    """
    Получает или возвращает закэшированный JWT-токен авторизации в iikoCloud (API v2).
    Токен действителен около 60 минут, кэшируется на 50 минут.
    """
    if os.path.exists(CACHE_PATH):
        try:
            with open(CACHE_PATH, 'r', encoding='utf-8') as cf:
                cdata = json.load(cf)
                if time.time() - cdata.get('ts', 0) < 3000:  # 50 минут
                    return cdata['token']
        except Exception:
            pass

    payload = {
        'apiLogin': CONFIG['apiKey'],
        'appId': CONFIG['appId'],
        'clientSecret': CONFIG['clientSecret']
    }
    req = urllib.request.Request(
        f"{CONFIG['apiBaseUrl']}/api/v2/access_token",
        data=json.dumps(payload).encode('utf-8'),
        headers={'Content-Type': 'application/json'}
    )
    try:
        with urllib.request.urlopen(req) as resp:
            token = json.loads(resp.read().decode())['token']
            with open(CACHE_PATH, 'w', encoding='utf-8') as cf:
                json.dump({'token': token, 'ts': time.time()}, cf)
            return token
    except urllib.error.HTTPError as e:
        err_body = e.read().decode('utf-8')
        if e.code == 423 and os.path.exists(CACHE_PATH):
            with open(CACHE_PATH, 'r', encoding='utf-8') as cf:
                return json.load(cf)['token']
        raise RuntimeError(f"HTTP {e.code} in get_token: {err_body}") from e

def post_api(endpoint, data, token):
    """
    Выполняет авторизованный POST-запрос к iikoCloud API.
    """
    req = urllib.request.Request(
        f"{CONFIG['apiBaseUrl']}{endpoint}",
        data=json.dumps(data).encode('utf-8'),
        headers={
            'Content-Type': 'application/json',
            'Authorization': f'Bearer {token}'
        }
    )
    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        err_msg = e.read().decode('utf-8')
        raise RuntimeError(f"HTTP {e.code} Error for {endpoint}: {err_msg}") from e

def create_delivery_order(order_data):
    """
    Создает заказ прямо на кассовый терминал iikoFront ресторана BEINI.
    ВНИМАНИЕ: Создает РЕАЛЬНЫЙ заказ в активную смену iikoFront!
    """
    token = get_token()
    
    order_type_id = CONFIG['orderTypes']['deliveryByCourier'] if order_data.get('type') == 'delivery' else CONFIG['orderTypes']['deliveryPickUp']
    
    payload = {
        "organizationId": CONFIG['organizationId'],
        "terminalGroupId": CONFIG['terminalGroupId'],
        "order": {
            "phone": order_data.get('phone'),
            "orderTypeId": order_type_id,
            "customer": {
                "name": order_data.get('name', 'Гость'),
                "type": "regular"
            },
            "deliveryPoint": {
                "address": {
                    "street": { "name": order_data.get('address', 'ул. С.С. Осканова, Назрань') }
                }
            } if order_data.get('type') == 'delivery' else None,
            "items": order_data.get('items', []),
            "payments": [
                {
                    "paymentTypeKind": "Cash",
                    "sum": order_data.get('totalPrice', 0),
                    "paymentTypeId": CONFIG['paymentTypes']['cash'],
                    "isPaid": False
                }
            ],
            "comment": order_data.get('comment', 'Заказ с сайта ресторана BEINI')
        }
    }
    
    res = post_api('/api/1/deliveries/create', payload, token)
    return res

if __name__ == '__main__':
    print("Проверка подключения к iikoCloud ресторана BEINI...")
    print(f"Конфиг: {CONFIG_PATH}")
    token = get_token()
    print("✓ Подключение успешно! JWT токен получен.")
    print(f"✓ Ресторан (organizationId): {CONFIG['organizationId']}")
    print(f"✓ Кассовый терминал (terminalGroupId): {CONFIG['terminalGroupId']}")

