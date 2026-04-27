import { expect, test } from '@playwright/test';
import { openMockedLoginPage } from './helpers/auth';
import {
  collectBrowserErrors,
  expectDocumentHasNoHorizontalOverflow,
  expectNoBrowserErrors,
  expectVisibleBoxInsideViewport,
} from './helpers/pageHealth';

test.describe('Авторизация', () => {
  test('показывает ошибки обязательных полей без запроса к API', async ({ page }) => {
    const browserErrors = collectBrowserErrors(page);

    await openMockedLoginPage(page);
    await page.getByRole('button', { name: 'Войти' }).click();

    await expect(page.getByText('Введите имя пользователя')).toBeVisible();
    await expect(page.getByText('Введите пароль')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.getByRole('heading', { name: 'MyHoreca XenionDesk' }), 'заголовок логина');
    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });

  test('отклоняет неверный пароль и логинится под admin/admin', async ({ page }) => {
    const browserErrors = collectBrowserErrors(page);

    await openMockedLoginPage(page);
    await page.getByPlaceholder('Имя пользователя').fill('admin');
    await page.getByPlaceholder('Пароль').fill('wrong');
    await page.getByRole('button', { name: 'Войти' }).click();

    await expect(page.getByText('Неверное имя пользователя или пароль')).toBeVisible();
    await expect(page).toHaveURL(/\/login(\?locale=ru)?$/);
    browserErrors.length = 0;

    await page.getByPlaceholder('Пароль').fill('admin');
    await page.getByRole('button', { name: 'Войти' }).click();

    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByText('Обзор системы')).toBeVisible();
    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });
});
