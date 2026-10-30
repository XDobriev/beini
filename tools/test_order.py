import json
import sys
import os

sys.stdout.reconfigure(encoding='utf-8')

# Гарантируем корректный импорт sync_iiko вне зависимости от рабочей директории
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from sync_iiko import get_token, post_api, CONFIG

def send_test_order():
    print("=" * 60)
    print("  ВНИМАНИЕ: ОТПРАВКА ТЕСТОВОГО ЗАКАЗА В РЕСТОРАН BEINI")
    print("  Этот скрипт отправляет РЕАЛЬНЫЙ заказ в iikoFront на кассу!")
    print("  НЕ ЗАПУСКАТЬ В ПРОДАКШЕНЕ БЕЗ СОГЛАСОВАНИЯ С ПЕРСОНАЛОМ.")
    print("=" * 60)
    
    print("\n1. Авторизация в iikoCloud (API v2)...")
    try:
        token = get_token()
        print("✓ Токен получен успешно.")
    except Exception as e:
        print(f"Ошибка авторизации: {e}")
        return

    # Тестовый заказ для кассы (Самовывоз) - Латте (250 руб)
    test_payload = {
        "organizationId": CONFIG["organizationId"],
        "terminalGroupId": CONFIG["terminalGroupId"],
        "createOrderSettings": {
            "transportToFrontTimeout": 30
        },
        "order": {
            "phone": "+79204827777",
            "orderTypeId": CONFIG["orderTypes"]["deliveryPickUp"], # Самовывоз
            "customer": {
                "name": "Хамзат",
                "type": "regular",
                "comment": "Тестовый заказ, не готовить"
            },
            "comment": "Тестовый заказ, не готовить",
            "items": [
                {
                    "productId": "c690faff-db1b-4b51-93cf-f89a461f71a9", # Латте
                    "price": 250,
                    "type": "Product",
                    "amount": 1,
                    "comment": "Тестовый заказ, не готовить"
                }
            ],
            "payments": [
                {
                    "paymentTypeKind": "Cash",
                    "sum": 250,
                    "paymentTypeId": CONFIG["paymentTypes"]["cash"],
                    "isPaid": False
                }
            ]
        }
    }

    print(f"\n2. Отправка заказа на кассу ресторана (terminalGroupId: {CONFIG['terminalGroupId']})...")
    try:
        res = post_api('/api/1/deliveries/create', test_payload, token)
        print("✓ Ответ сервера iikoCloud получен!")
        corr_id = res.get("correlationId")
        print("  Correlation ID (ID очереди):", corr_id)
        
        order_info = res.get("orderInfo", {})
        creation_status = order_info.get("creationStatus")
        
        print(f"  Статус создания в iiko: {creation_status}")
        
        if creation_status == "Success" or creation_status == "InProgress":
            print("\n" + "=" * 60)
            print("✓ ЗАКАЗ УСПЕШНО ПЕРЕДАН НА КАССУ iikoFront!")
            print("  Проверьте терминал: вкладка 'Доставка' / звуковой сигнал / чек на принтере.")
            print("=" * 60)
        elif creation_status == "Error":
            err_info = order_info.get("errorInfo", {})
            err_code = err_info.get("code")
            err_desc = err_info.get("message") or err_info.get("description")
            print(f"\n[!] Внимание: касса вернула статус ошибки: {err_code} ({err_desc})")
            if err_code == "TerminalGroupDisabled":
                print("  Причина: Кассовый терминал в ресторане сейчас выключен, находится в спящем режиме или закрыта кассовая смена.")
                print("  В рабочее время при включенном iikoFront заказ сразу перейдет в статус Success.")
            print("=" * 60)
        else:
            print("  Детали ответа:", json.dumps(res, ensure_ascii=False, indent=2))
            
    except Exception as e:
        print("Ошибка при отправке заказа:", e)

if __name__ == '__main__':
    send_test_order()
