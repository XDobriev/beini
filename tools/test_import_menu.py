#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
tools/test_import_menu.py — Автоматические тесты для импортёра и валидатора меню BEINI.
"""

import json
import sys
import tempfile
import unittest
from pathlib import Path

# Добавляем tools/ в sys.path для прямого импорта
sys.path.insert(0, str(Path(__file__).resolve().parent))

from import_menu import (
    ALLOWED_CATEGORIES,
    audit_catalog,
    export_js_menu_data,
    reconcile_menu,
    validate_dish,
)


class TestMenuImport(unittest.TestCase):

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
                    'type': 'Dish',
                    'sizePrices': [{'price': {'currentPrice': 620.0}}]
                },
                # Удаленное блюдо
                {
                    'id': '11111111-2222-3333-4444-555555555555',
                    'name': 'Удаленное блюдо',
                    'isDeleted': True,
                    'type': 'Dish',
                    'sizePrices': [{'price': {'currentPrice': 400.0}}]
                },
                # Новое блюдо в iiko
                {
                    'id': '99999999-8888-7777-6666-555555555555',
                    'name': 'Новое блюдо от шефа',
                    'isDeleted': False,
                    'type': 'Dish',
                    'parentGroup': 'grp-1',
                    'sizePrices': [{'price': {'currentPrice': 750.0}}]
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
