import { expect, test } from '@playwright/test';
import { loginAsAdmin } from './helpers/auth';
import {
  collectBrowserErrors,
  expectDocumentHasNoHorizontalOverflow,
  expectNoBrowserErrors,
  expectVisibleBoxInsideViewport,
  expectVisibleControlsInsideViewport,
} from './helpers/pageHealth';

test.describe('Адаптивность и вёрстка', () => {
  test('сохраняет рабочую компоновку dashboard на текущем viewport', async ({ page }) => {
    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);

    await expect(page.getByText('Всего тикетов')).toBeVisible();
    await expect(page.getByText('Принято звонков за 24 часа')).toBeVisible();
    await expect(page.getByText('Опросы серверов за 24 часа')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.app-main-header'), 'верхняя панель');
    await expectVisibleBoxInsideViewport(page.locator('.app-header-right'), 'профиль и настройки');
    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });

  test('открывает страницу тикетов на узком viewport без горизонтального скролла документа', async ({ page }, testInfo) => {
    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);
    await page.goto('/tickets');

    if (testInfo.project.name.includes('mobile')) {
      const firstCard = page.locator('.ticket-mobile-card').first();
      await expect(firstCard).toBeVisible();
      await expectVisibleBoxInsideViewport(firstCard, 'первая мобильная карточка тикета');
      await expectVisibleControlsInsideViewport(firstCard, 'первая мобильная карточка тикета');
    } else {
      await expect(page.getByRole('table')).toBeVisible();
    }
    await expect(page.getByText('Ресторан Север')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.app-main-header'), 'верхняя панель');
    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });
});
