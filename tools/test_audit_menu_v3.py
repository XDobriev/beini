import copy
import importlib.util
import pathlib
import unittest


class MenuV3AuditTests(unittest.TestCase):
    def audit(self, menu, seed=None, stops=None):
        path = pathlib.Path(__file__).with_name('audit_menu_v3.py')
        self.assertTrue(path.exists(), 'read-only v3 auditor is missing')
        spec = importlib.util.spec_from_file_location('audit_menu_v3', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module.audit_menu(menu, seed or self.seed(), stops)

    def menu(self):
        return {'id': 'menu', 'name': 'Menu', 'products': [
            {'id': 'one', 'orderItemType': 'Product'},
            {'id': 'two', 'orderItemType': 'Product'},
        ], 'itemsGroups': [{'id': 'g', 'items': [
            {'productId': 'one', 'name': 'Рибай', 'sizePrices': [{'price': 100, 'menuItemSizeId': 'record-only'}]},
            {'productId': 'two', 'name': 'Рибай', 'sizePrices': [{'price': 200}]},
        ]}]}

    def seed(self):
        return {'items': [{'id': 'local', 'iikoId': 'one', 'sourceName': 'Рибай', 'name': 'Редакторское имя'}]}

    def test_wrong_identity_is_reported_without_remapping(self):
        menu = self.menu()
        menu['itemsGroups'][0]['items'][0]['name'] = 'Творожные шарики'
        result = self.audit(menu)
        self.assertEqual(result['bindings'][0]['status'], 'identity_review_required')
        self.assertEqual(result['bindings'][0]['iikoId'], 'one')
        self.assertEqual(len(result['missing']), 1)

    def test_distinct_ids_same_name_and_menu_record_are_not_variants(self):
        result = self.audit(self.menu(), {'items': []})
        self.assertEqual(len(result['missing']), 2)
        self.assertTrue(all(p['supported'] for p in result['products']))
        self.assertTrue(all(p['available'] is None for p in result['products']))

    def test_format_constraints_and_explicit_stop_scope_are_separate(self):
        menu = self.menu()
        menu['products'][0]['canSetOpenPrice'] = True
        menu['itemsGroups'][0]['items'][1]['modifierGroups'] = [{'items': []}]
        stops = {'one': False, 'two': True}
        result = self.audit(menu, stops=stops)
        self.assertEqual(result['products'][0]['reasons'], ['open_price'])
        self.assertEqual(result['products'][1]['reasons'], ['modifiers'])
        self.assertFalse(result['products'][1]['available'])
        self.assertTrue(result['products'][1]['stopped'])

    def test_cosmetic_name_and_absent_id(self):
        seed = self.seed()
        seed['items'][0]['sourceName'] = '  РИБАЙ  '
        seed['items'].append({'id': 'old', 'iikoId': 'gone', 'sourceName': 'Old'})
        result = self.audit(self.menu(), seed)
        self.assertEqual([b['status'] for b in result['bindings']], ['matched', 'missing_id'])

    def test_hidden_editorial_is_not_available(self):
        seed = self.seed()
        seed['items'][0]['hidden'] = True
        result = self.audit(self.menu(), seed, {})
        self.assertFalse(result['bindings'][0]['available'])

    def test_incomplete_dictionary_is_rejected(self):
        menu = self.menu()
        menu['products'].pop()
        with self.assertRaises(ValueError):
            self.audit(menu)

    def test_hidden_or_scheduled_group_does_not_override_visible_identity(self):
        for restricted in [{'isHidden': True}, {'scheduleId': 'breakfast'}]:
            for first in [True, False]:
                menu = self.menu()
                hidden = {'id': 'restricted', **restricted, 'items': [
                    {'productId': 'one', 'name': 'Другой текст', 'sizePrices': [{'price': 900}]}]}
                menu['itemsGroups'].insert(0 if first else 1, hidden)
                result = self.audit(menu)
                row = next(p for p in result['products'] if p['iikoId'] == 'one')
                self.assertTrue(row['supported'])
                self.assertEqual(row['name'], 'Рибай')
                self.assertEqual(result['bindings'][0]['status'], 'matched')

    def test_stop_scope_missing_is_not_empty(self):
        path = pathlib.Path(__file__).with_name('audit_menu_v3.py')
        self.assertTrue(path.exists(), 'read-only v3 auditor is missing')
        spec = importlib.util.spec_from_file_location('audit_menu_v3', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with self.assertRaises(ValueError):
            module.selected_stops({'terminalGroupStopLists': []}, 'org', 'terminal')
        response = {'terminalGroupStopLists': [{'organizationId': 'org', 'items': [{'terminalGroupId': 'terminal', 'items': []}]}]}
        self.assertEqual(module.selected_stops(response, 'org', 'terminal'), {})


if __name__ == '__main__':
    unittest.main()
