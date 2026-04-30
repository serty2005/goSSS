import { expect, test } from '@playwright/test';
import { loginAsAdmin } from './helpers/auth';
import {
  collectBrowserErrors,
  expectDocumentHasNoHorizontalOverflow,
  expectNoBrowserErrors,
  expectVisibleBoxInsideViewport,
} from './helpers/pageHealth';

test.describe('Основной интерфейс ServiceDesk', () => {
  test('проверяет shell, навигацию, таблицу тикетов и быстрый просмотр', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий с полной боковой навигацией проверяется на desktop-проекте');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);

    await expect(page.getByRole('menuitem', { name: 'Тикеты' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Компании' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Оборудование' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Администрирование' })).toBeVisible();

    await expectVisibleBoxInsideViewport(page.locator('.app-main-header'), 'верхняя панель');
    await expectVisibleBoxInsideViewport(page.locator('.app-header-right'), 'профиль и настройки');
    await expectDocumentHasNoHorizontalOverflow(page);

    await page.getByRole('menuitem', { name: 'Тикеты' }).click();
    await expect(page).toHaveURL(/\/tickets$/);
    await expect(page.getByText('Ресторан Север')).toBeVisible();
    await expect(page.locator('.tickets-table')).toBeVisible();
    await expect(page.getByText('Касса не печатает чек после обновления смены.')).toBeVisible();
    await expect(page.getByText('Показано: 2 из 2')).toBeVisible();

    await expectDocumentHasNoHorizontalOverflow(page);

    await page.getByRole('row', { name: /1001/ }).click();
    await expect(page.getByText('Быстрый просмотр #1001')).toBeVisible();
    const quickPreview = page.getByLabel('Быстрый просмотр #1001');
    await expect(quickPreview.getByText('Проверить доступность фискального регистратора')).toBeVisible();
    await expect(quickPreview.getByText('Подключения')).toBeVisible();
    await expectDocumentHasNoHorizontalOverflow(page);

    await page.keyboard.press('Escape');
    await expect(page.getByText('Быстрый просмотр #1001')).toBeHidden();
    expectNoBrowserErrors(browserErrors);
  });

  test('открывает настройки темы без смещения header', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Полный popover темы проверяется на desktop-проекте');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);
    await page.getByRole('button', { name: 'Открыть настройки оформления' }).click();

    await expect(page.getByText('Оформление')).toBeVisible();
    await expect(page.getByText('День')).toBeVisible();
    await expect(page.getByText('Ночь')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.ant-popover').last(), 'popover настроек темы');
    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });
});
