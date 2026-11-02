"""Read-only, offline v3 menu audit. Never writes a seed or calls order APIs."""
import argparse
import json
import math
from pathlib import Path


def normalized_name(value):
    return ' '.join(value.lower().replace('ё', 'е').split())


def selected_stops(response, organization_id, terminal_group_id):
    organizations = [o for o in response.get('terminalGroupStopLists', [])
                     if o.get('organizationId') == organization_id]
    if len(organizations) != 1 or not isinstance(organizations[0].get('items'), list):
        raise ValueError('selected organization stop-list missing or duplicated')
    terminals = [t for t in organizations[0]['items'] if t.get('terminalGroupId') == terminal_group_id]
    if len(terminals) != 1 or not isinstance(terminals[0].get('items'), list):
        raise ValueError('selected terminal stop-list missing or duplicated')
    result = {}
    for item in terminals[0]['items']:
        key = item.get('productId', '').strip().lower()
        balance = item.get('balance')
        if (not key or key in result or isinstance(balance, bool)
                or not isinstance(balance, (int, float)) or not math.isfinite(balance)
                or item.get('sizeId') is not None):
            raise ValueError('invalid stop-list item')
        result[key] = balance <= 0
    return result


def audit_menu(menu, seed, stops=None):
    if (not menu.get('id') or not menu.get('name')
            or not isinstance(menu.get('products'), list)
            or not isinstance(menu.get('itemsGroups'), list)):
        raise ValueError('incomplete external menu')
    dictionary = {}
    for product in menu['products']:
        key = product.get('id', '').strip().lower()
        if not key or key in dictionary:
            raise ValueError('invalid or duplicate product identity')
        dictionary[key] = product
    rows = {}
    restricted_items = {}
    groups = set()
    for group in menu['itemsGroups']:
        if not group.get('id') or group['id'] in groups:
            raise ValueError('invalid or duplicate group identity')
        groups.add(group['id'])
        if group.get('isHidden') or group.get('scheduleId'):
            for item in group.get('items', []):
                key = item.get('productId', '').strip().lower()
                if key in dictionary:
                    restricted_items[key] = (item, group)
            continue
        for item in group.get('items', []):
            key = item.get('productId', '').strip().lower()
            if not key:
                continue  # Compound item: current order contract cannot represent it.
            if key not in dictionary:
                raise ValueError('product dictionary incomplete')
            product = dictionary[key]
            reasons = []
            if group.get('isHidden'):
                reasons.append('hidden_group')
            if group.get('scheduleId'):
                reasons.append('group_schedule')
            if item.get('isHidden'):
                reasons.append('hidden_item')
            if item.get('modifierGroups') or product.get('modifierSchemaId'):
                reasons.append('modifiers')
            for field, reason in [('canSetOpenPrice', 'open_price'), ('useBalanceForSell', 'weight_sale'),
                                  ('splittable', 'splittable'), ('isMarked', 'marked')]:
                if product.get(field):
                    reasons.append(reason)
            if product.get('orderItemType') != 'Product':
                reasons.append('order_item_type')
            if product.get('type', '') not in ('', None, 'DISH', 'GOODS'):
                reasons.append('product_type')
            sizes = item.get('sizePrices', [])
            if len(sizes) != 1 or any(s.get('sizeId') for s in sizes):
                reasons.append('size_variants')
            if any(s.get('isHidden') for s in sizes):
                reasons.append('hidden_price')
            prices = [s.get('price') for s in sizes]
            if not prices or any(isinstance(p, bool) or not isinstance(p, (int, float))
                                 or not math.isfinite(p) or p <= 0 for p in prices):
                reasons.append('nonpositive_price')
            if not item.get('name', '').strip():
                raise ValueError('missing item name')
            if key in rows:
                rows[key]['reasons'].append('ambiguous_product')
                rows[key]['supported'] = rows[key]['available'] = False
                continue
            stopped = stops.get(key, False) if stops is not None else None
            rows[key] = {'iikoId': key, 'name': item['name'], 'group': group.get('name', ''),
                         'price': prices[0] if len(prices) == 1 else None,
                         'reasons': reasons, 'supported': not reasons, 'stopped': stopped,
                         'available': (not reasons and not stopped) if stops is not None else None}
    for key in dictionary.keys() - rows.keys():
        item, group = restricted_items.get(key, ({}, {}))
        reason = 'hidden_group' if group.get('isHidden') else ('group_schedule' if group.get('scheduleId') else 'not_in_visible_group')
        rows[key] = {'iikoId': key, 'name': item.get('name', ''), 'group': group.get('name', ''), 'price': None,
                     'reasons': [reason], 'supported': False,
                     'stopped': stops.get(key, False) if stops is not None else None,
                     'available': False}
    bindings = []
    ids = set()
    for item in seed['items']:
        key = item['iikoId'].strip().lower()
        ids.add(key)
        row = rows.get(key)
        source = item.get('sourceName', '')
        status = 'missing_id' if row is None else (
            'matched' if source and normalized_name(source) == normalized_name(row['name'])
            else 'identity_review_required')
        available = row['available'] if row else False
        if status != 'matched' or item.get('hidden') or item.get('available') is False:
            available = False
        bindings.append({'id': item['id'], 'iikoId': key, 'status': status,
                         'available': available, 'actualName': row['name'] if row else ''})
    return {'products': list(rows.values()), 'bindings': bindings,
            'missing': [row for key, row in rows.items() if key not in ids],
            'stopListKnown': stops is not None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--from-file', required=True, type=Path)
    parser.add_argument('--seed', type=Path, default=Path(__file__).resolve().parents[1] / 'data/menu_catalog.json')
    parser.add_argument('--stop-list-file', type=Path)
    parser.add_argument('--organization-id')
    parser.add_argument('--terminal-group-id')
    args = parser.parse_args()
    try:
        data = json.loads(args.from_file.read_text(encoding='utf-8-sig'))
        menu = data.get('menu', data)
        stops = None
        if args.stop_list_file:
            if not args.organization_id or not args.terminal_group_id:
                parser.error('stop-list requires explicit organization and terminal scope')
            stops = selected_stops(json.loads(args.stop_list_file.read_text(encoding='utf-8-sig')),
                                   args.organization_id, args.terminal_group_id)
        result = audit_menu(menu, json.loads(args.seed.read_text(encoding='utf-8-sig')), stops)
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return int(any(b['status'] != 'matched' for b in result['bindings']))
    except (ValueError, KeyError, TypeError, OSError):
        parser.exit(2, 'Menu audit failed: invalid or unreadable input\n')


if __name__ == '__main__':
    raise SystemExit(main())
