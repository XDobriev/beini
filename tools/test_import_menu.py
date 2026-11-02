#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
tools/test_import_menu.py — Автоматические тесты для импортёра и валидатора меню BEINI.
"""

import json
import io
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import URLError
from pathlib import Path

# Добавляем tools/ в sys.path для прямого импорта
sys.path.insert(0, str(Path(__file__).resolve().parent))

from import_menu import (
    ALLOWED_CATEGORIES,
    apply_reconciliation,
    product_price,
    fetch_live_stop_lists,
    parse_stop_lists,
    audit_catalog,
    export_js_menu_data,
    reconcile_menu,
    validate_dish,
)


class TestMenuImport(unittest.TestCase):

    def test_legacy_apply_cannot_rewrite_reviewed_v3_seed_or_fetch(self):
        import import_menu
        catalog_path = self.root / 'data' / 'menu_catalog.json'
        catalog_path.parent.mkdir()
        (self.root / 'js').mkdir()
        original = json.dumps({'categories': [], 'items': [dict(self.valid_dish, sourceName='Люля-кебаб')]}).encode()
        catalog_path.write_bytes(original)
        with patch.object(import_menu, 'get_repo_root', return_value=self.root), \
                patch.object(sys, 'argv', ['import_menu.py', '--fetch', '--apply']), \
                patch.object(import_menu, 'load_config', return_value={}), \
                patch.object(import_menu, 'fetch_live_nomenclature', return_value={'revision': 1, 'products': [{'id': self.valid_dish['iikoId'], 'isDeleted': True}]}) as fetch, \
                patch.object(import_menu, 'fetch_live_stop_lists', return_value=set()), \
                patch.object(sys, 'stdout', io.StringIO()), patch.object(sys, 'stderr', io.StringIO()):
            with self.assertRaises(SystemExit) as result:
                import_menu.main()
            self.assertEqual(result.exception.code, 2)
            fetch.assert_not_called()
        self.assertEqual(catalog_path.read_bytes(), original)

    def test_export_preserves_new_categories_and_hides_new_products(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / 'menu.js'
            catalog = {'categories': [{'id': 'breakfasts', 'name': 'Завтраки', 'sortOrder': 1}],
                       'items': [dict(self.valid_dish, id='breakfast', category='breakfasts'),
                                 dict(self.valid_dish, id='new', category='breakfasts', hidden=True)]}
            export_js_menu_data(catalog, output)
            text = output.read_text(encoding='utf-8')
            self.assertIn('breakfast', text)
            self.assertNotIn('"id": "new"', text)

    def test_official_sale_flags(self):
        product = {
            'type': 'dish', 'isDeleted': False, 'useBalanceForSell': False,
            'canSetOpenPrice': False, 'orderItemType': 'Product',
            'sizePrices': [{'sizeId': None, 'price': {'currentPrice': 620, 'isIncludedInMenu': True}}]
        }
        self.assertEqual(product_price(product), 620)
        for field in ('useBalanceForSell', 'canSetOpenPrice'):
            self.assertEqual(product_price(dict(product, **{field: True})), 0)
        self.assertEqual(product_price(dict(product, orderItemType='Compound')), 0)
        del product['useBalanceForSell']
        self.assertEqual(product_price(product), 0)

    def setUp(self):
        self.tmp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp_dir.name)

        # Создаем тестовую структуру файлов
        (self.root / 'assets' / 'images' / 'dishes').mkdir(parents=True, exist_ok=True)
        self.sample_img = self.root / 'assets' / 'images' / 'dishes' / 'test_dish.jpg'
        self.sample_img.write_text('dummy image bytes')

        self.valid_dish = {
            'id': 'beini-test-01',
            'iikoId': '0ce65922-6a9f-453a-bb92-b7c35b4950bb',
            'name': 'Люля-кебаб из телятины',
            'description': 'Вкусный люля на углях',
            'price': 590,
            'weight': '410 г',
            'category': 'grill',
            'image': 'assets/images/dishes/test_dish.jpg',
            'badge': 'Хит',
            'isHalal': True,
            'available': True
        }

    def tearDown(self):
        self.tmp_dir.cleanup()

    def test_validate_dish_valid(self):
        errors = validate_dish(self.valid_dish, self.root)
        self.assertEqual(errors, [], "Валидное блюдо должно проходить без ошибок")

    def test_validate_dish_missing_iiko_id(self):
        dish = dict(self.valid_dish)
        dish['iikoId'] = ''
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('Отсутствует обязательный iiko ID' in e for e in errors))

        dish['iikoId'] = 'not-a-valid-uuid'
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('Некорректный формат iiko ID' in e for e in errors))

    def test_validate_dish_invalid_price(self):
        dish = dict(self.valid_dish)
        dish['price'] = 0
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('больше нуля' in e for e in errors))

        dish['price'] = -100
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('больше нуля' in e for e in errors))

        dish['price'] = None
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('Отсутствует цена' in e for e in errors))

    def test_validate_dish_missing_category(self):
        dish = dict(self.valid_dish)
        dish['category'] = ''
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('Не назначена категория' in e for e in errors))

        dish['category'] = 'unknown_category_xyz'
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('Неизвестная категория' in e for e in errors))

    def test_validate_dish_missing_image(self):
        dish = dict(self.valid_dish)
        dish['image'] = 'assets/images/dishes/non_existent.jpg'
        errors = validate_dish(dish, self.root)
        self.assertTrue(any('не найден на диске' in e for e in errors))

    def test_audit_catalog(self):
        catalog = {
            'items': [
                self.valid_dish,
                {
                    'id': 'bad-dish',
                    'iikoId': '',
                    'name': 'Плохое блюдо',
                    'price': 0,
                    'category': '',
                    'image': ''
                }
            ]
        }
        res = audit_catalog(catalog, self.root)
        self.assertEqual(res['total'], 2)
        self.assertEqual(res['validCount'], 1)
        self.assertEqual(res['invalidCount'], 1)
        self.assertEqual(len(res['invalidItems'][0]['errors']), 4)

    def test_reconcile_menu_categories(self):
        local_catalog = {
            'items': [
                self.valid_dish,
                {
                    'id': 'beini-deleted-01',
                    'iikoId': '11111111-2222-3333-4444-555555555555',
                    'name': 'Удаленное блюдо',
                    'price': 400,
                    'category': 'soups',
                    'image': 'assets/images/dishes/test_dish.jpg'
                }
            ]
        }

        nomenclature_data = {
            'products': [
                # Существующее блюдо с новой ценой
                {
                    'id': '0ce65922-6a9f-453a-bb92-b7c35b4950bb',
                    'name': 'Люля-кебаб из телятины',
                    'isDeleted': False,
                    'type': 'dish',
                    'useBalanceForSell': False, 'canSetOpenPrice': False,
                    'sizePrices': [{'price': {'currentPrice': 620.0, 'isIncludedInMenu': True}}]
                },
                # Удаленное блюдо
                {
                    'id': '11111111-2222-3333-4444-555555555555',
                    'name': 'Удаленное блюдо',
                    'isDeleted': True,
                    'type': 'dish',
                    'useBalanceForSell': False, 'canSetOpenPrice': False,
                    'sizePrices': [{'price': {'currentPrice': 400.0, 'isIncludedInMenu': True}}]
                },
                # Новое блюдо в iiko
                {
                    'id': '99999999-8888-7777-6666-555555555555',
                    'name': 'Новое блюдо от шефа',
                    'isDeleted': False,
                    'type': 'dish',
                    'useBalanceForSell': False, 'canSetOpenPrice': False,
                    'parentGroup': 'grp-1',
                    'sizePrices': [{'price': {'currentPrice': 750.0, 'isIncludedInMenu': True}}]
                }
            ],
            'groups': [{'id': 'grp-1', 'name': 'Мангал'}],
            'productCategories': []
        }

        # Блюдо valid_dish добавим в стоп-лист
        stopped_ids = {'0ce65922-6a9f-453a-bb92-b7c35b4950bb'}

        res = reconcile_menu(local_catalog, nomenclature_data, stopped_ids, self.root)

        # 1. Добавленные: 1 позиция (Новое блюдо от шефа)
        self.assertEqual(len(res['added']), 1)
        self.assertEqual(res['added'][0]['iikoId'], '99999999-8888-7777-6666-555555555555')

        # 2. Скрытые / удаленные: 1 позиция (Удаленное блюдо)
        self.assertEqual(len(res['hidden']), 1)
        self.assertEqual(res['hidden'][0]['item']['id'], 'beini-deleted-01')

        # 3. Недоступные (в стоп-листе): 1 позиция (Люля-кебаб)
        self.assertEqual(len(res['unavailable']), 1)
        self.assertEqual(res['unavailable'][0]['item']['id'], 'beini-test-01')

        # 4. Изменение цены: 1 позиция (590 -> 620)
        self.assertEqual(len(res['priceChanged']), 1)
        self.assertEqual(res['priceChanged'][0]['oldPrice'], 590)
        self.assertEqual(res['priceChanged'][0]['newPrice'], 620.0)

    def fixtures(self):
        root = Path(__file__).resolve().parents[1] / 'internal' / 'menusync' / 'testdata'
        return (json.loads((root / 'nomenclature.json').read_text(encoding='utf-8')),
                json.loads((root / 'stop_lists.json').read_text(encoding='utf-8')))

    def test_stop_list_terminal_scope_and_explicit_empty(self):
        _, stops = self.fixtures()
        first = '11111111-1111-4111-8111-111111111111'
        self.assertEqual(parse_stop_lists(stops, 'org', 'terminal'), {first})
        self.assertNotIn(first, parse_stop_lists(stops, 'org', 'other'))
        stops['terminalGroupStopLists'][0]['items'][1]['items'] = []
        self.assertEqual(parse_stop_lists(stops, 'org', 'terminal'), set())

    def test_invalid_stop_list_never_becomes_empty(self):
        for bad in ({}, {'terminalGroupStopLists': []}, {'errorCode': 'failure'},
                    {'terminalGroupStopLists': None}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                parse_stop_lists(bad, 'org', 'terminal')
        _, stops = self.fixtures()
        stops['terminalGroupStopLists'][0]['items'][1]['items'] = None
        with self.assertRaises(ValueError):
            parse_stop_lists(stops, 'org', 'terminal')
        with self.assertRaises(ValueError):
            parse_stop_lists(stops, 'org', '')

    def test_stop_list_network_error_propagates(self):
        with patch('import_menu.fetch_token', return_value='fixture-token'), \
                patch('import_menu.urllib.request.urlopen', side_effect=URLError('offline')):
            with self.assertRaises(URLError):
                fetch_live_stop_lists({'organizationId': 'org', 'terminalGroupId': 'terminal'})
        with self.assertRaises(ValueError):
            fetch_live_stop_lists({'organizationId': 'org'})

    def test_apply_preserves_media_and_exclusions(self):
        nomenclature, stops = self.fixtures()
        dish = dict(self.valid_dish, iikoId='11111111-1111-4111-8111-111111111111')
        excluded = dict(self.valid_dish, id='excluded', available=False,
                        iikoId='22222222-2222-4222-8222-222222222222')
        local = {'items': [dish, excluded], 'categories': [{'id': 'grill'}]}
        result = apply_reconciliation(local, nomenclature, set())
        self.assertEqual(len(result['items']), 2)  # no automatic new positions
        self.assertEqual(result['items'][0]['price'], 620)
        for field in ('name', 'description', 'image', 'weight', 'badge', 'isHalal'):
            self.assertEqual(result['items'][0][field], dish[field])
        self.assertFalse(result['items'][1]['available'])
        self.assertEqual(local['items'][0]['price'], 590)  # no in-place mutation
        result = apply_reconciliation(local, nomenclature, parse_stop_lists(stops, 'org', 'terminal'))
        self.assertFalse(result['items'][0]['available'])
        result = apply_reconciliation(result, nomenclature, None)
        self.assertFalse(result['items'][0]['available'])  # unknown stop-list cannot restore
        report = reconcile_menu(local, nomenclature, None, self.root)
        self.assertFalse(report['stopListKnown'])

    def test_removed_zero_sized_and_malformed_menu(self):
        nomenclature, _ = self.fixtures()
        dish = dict(self.valid_dish, iikoId='11111111-1111-4111-8111-111111111111')
        for kind in ('removed', 'zero', 'sized', 'modifier'):
            data = json.loads(json.dumps(nomenclature))
            p = data['products'][0]
            if kind == 'removed':
                p['isDeleted'] = True
            elif kind == 'zero':
                p['sizePrices'][0]['price']['currentPrice'] = 0
            elif kind == 'sized':
                p['sizePrices'][0]['sizeId'] = 'size'
            else:
                p['modifiers'] = [{'minAmount': 1}]
            result = apply_reconciliation({'items': [dish]}, data, set())
            self.assertFalse(result['items'][0]['available'])
        for bad in ({}, {'products': []}, {'products': [{}]}):
            with self.assertRaises(ValueError):
                apply_reconciliation({'items': [dish]}, bad, set())
        nomenclature['products'][0]['sizePrices'][0]['price']['currentPrice'] = float('nan')
        with self.assertRaises(ValueError):
            apply_reconciliation({'items': [dish]}, nomenclature, set())


    def test_export_js_menu_data(self):
        catalog = {
            'items': [self.valid_dish]
        }
        out_js = self.root / 'menu-data.js'
        export_js_menu_data(catalog, out_js)
        self.assertTrue(out_js.exists())

        content = out_js.read_text(encoding='utf-8')
        self.assertIn('const MENU_DATA =', content)
        self.assertIn('0ce65922-6a9f-453a-bb92-b7c35b4950bb', content)
        self.assertIn('Люля-кебаб из телятины', content)


if __name__ == '__main__':
    unittest.main()
