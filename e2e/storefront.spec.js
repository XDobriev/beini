const { test, expect } = require('@playwright/test');

test('renders single active category tab without placeholder images and switches tabs', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', error => pageErrors.push(error.message));

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Меню' })).toBeVisible();

  // По умолчанию рендерится ровно один раздел меню (Национальная и восточная кухня)
  const sections = page.locator('#menu-sections .menu-cat');
  await expect(sections).toHaveCount(1);
  await expect(sections.first().locator('.h3')).toContainText('Национальная и восточная кухня');

  // Пустые заглушки placeholder.svg не рендерятся как <img>
  await expect(page.locator('#menu-sections img[src*="placeholder.svg"]')).toHaveCount(0);

  // Кнопка следующего раздела внизу списка переключает на «Мангал и стейки»
  const nextBtn = page.locator('.menu-pager [data-action="jump"]');
  await expect(nextBtn).toContainText('Следующий раздел: Мангал и стейки');
  await nextBtn.click();

  await expect(sections).toHaveCount(1);
  await expect(sections.first().locator('.h3')).toContainText('Мангал и стейки');

  // Поиск ищет сквозь все разделы меню
  const searchInput = page.locator('#menu-search');
  await searchInput.fill('лимонад');
  await expect(page.locator('#menu-sections .dish-with-variants').first()).toBeVisible();

  // Очистка поиска возвращает режим одного раздела
  await page.locator('#search-clear').click();
  await expect(sections).toHaveCount(1);
  expect(pageErrors).toEqual([]);
});

test('guest can switch to drinks category, select a portion and add it to the cart', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', error => pageErrors.push(error.message));

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Меню' })).toBeVisible();

  const drinksBtn = page.locator('[data-action="jump"][data-cat="drinks"]:visible').first();
  await drinksBtn.click();

  const dish = page.locator('.dish-with-variants').first();
  const group = dish.locator('.dish-variants');
  const variants = group.locator('button.dish-variant:not([disabled])');
  await expect(variants).toHaveCount(2);
  const secondVariant = variants.nth(1);
  await secondVariant.click();
  await expect(secondVariant).toHaveAttribute('aria-pressed', 'true');

  await dish.getByRole('button', { name: /Добавить в заказ:/ }).click();
  await expect(page.locator('#header-cart .cart-count')).toHaveText('1');
  await expect(dish.locator('.stepper')).toContainText('1');

  const mock = await page.evaluate(() => fetch('/__mock__').then(response => response.json()));
  expect(mock.creates).toBe(0);
  expect(pageErrors).toEqual([]);
});

test('mobile guest can open and close the cart sheet without submitting', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile-chromium');

  await page.goto('/');
  await page.getByRole('button', { name: /Добавить в заказ:/ }).first().click();
  const cartBar = page.locator('#cart-bar');
  await expect(cartBar).toBeVisible();
  await cartBar.getByRole('button').click();

  const cart = page.getByRole('dialog', { name: 'Ваш заказ' });
  await expect(cart).toBeVisible();
  await expect(cart.locator('.sheet-row img[src*="placeholder.svg"]')).toHaveCount(0);
  await cart.getByRole('button', { name: 'Закрыть' }).click();
  await expect(cart).toBeHidden();

  const mock = await page.evaluate(() => fetch('/__mock__').then(response => response.json()));
  expect(mock.creates).toBe(0);
});
