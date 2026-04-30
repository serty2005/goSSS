import { expect, test } from '@playwright/test';
import { loginAsAdmin } from './helpers/auth';
import {
  collectBrowserErrors,
  expectDocumentHasNoHorizontalOverflow,
  expectNoBrowserErrors,
  expectVisibleBoxInsideViewport,
} from './helpers/pageHealth';

test.describe('Desktop-регрессии ServiceDesk', () => {
  test('фиксирует боковое меню и показывает уведомления ниже header', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-компоновку');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);

    const sidebar = page.locator('.ant-layout-sider').first();
    const header = page.locator('.app-main-header');
    await expectVisibleBoxInsideViewport(sidebar, 'левое меню');
    await expectVisibleBoxInsideViewport(header, 'верхняя панель');

    const topBefore = await sidebar.evaluate((node) => Math.round(node.getBoundingClientRect().top));
    await page.evaluate(() => window.scrollTo(0, 520));
    const topAfter = await sidebar.evaluate((node) => Math.round(node.getBoundingClientRect().top));
    expect(Math.abs(topAfter - topBefore), 'левое меню должно оставаться закреплённым при прокрутке').toBeLessThanOrEqual(1);

    await page.goto('/tickets/ticket-1001');
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: async () => undefined },
        configurable: true,
      });
    });
    await page.getByRole('row', { name: /Контакт/ }).getByRole('button', { name: /copy Копировать/ }).click();

    const notice = page.locator('.ant-message-notice-content').last();
    await expect(notice).toBeVisible();
    const placement = await notice.evaluate((node) => {
      const box = node.getBoundingClientRect();
      return {
        top: Math.round(box.top),
        width: Math.round(box.width),
        centerDelta: Math.round(Math.abs((box.left + box.width / 2) - window.innerWidth / 2)),
      };
    });
    expect(placement.top, 'уведомление должно появляться в открытой зоне сразу под header').toBeGreaterThanOrEqual(56);
    expect(placement.top, 'уведомление должно появляться в открытой зоне сразу под header').toBeLessThanOrEqual(96);
    expect(placement.width, 'уведомление должно быть компактным, а не растянутым на весь экран').toBeLessThanOrEqual(560);
    expect(placement.centerDelta, 'уведомление должно быть по центру области под header').toBeLessThanOrEqual(8);

    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });

  test('выравнивает верхнюю строку новой заявки и сохраняет ручной телефон контакта', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-модалку');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);
    await page.goto('/tickets');

    await page.getByRole('button', { name: 'Новая заявка' }).click();
    const dialog = page.getByRole('dialog', { name: 'Новая заявка' });
    await expect(dialog).toBeVisible();

    const phoneItem = dialog.locator('.ant-form-item').filter({ hasText: 'Номер телефона' }).first();
    const nameItem = dialog.locator('.ant-form-item').filter({ hasText: 'Имя контакта' }).first();
    const boxes = await Promise.all([phoneItem.boundingBox(), nameItem.boundingBox()]);
    expect(boxes[0]).not.toBeNull();
    expect(boxes[1]).not.toBeNull();
    expect(Math.abs((boxes[0]?.y || 0) - (boxes[1]?.y || 0)), 'поля телефона и имени должны быть в одной строке').toBeLessThanOrEqual(4);
    expect(Math.abs((boxes[0]?.width || 0) - (boxes[1]?.width || 0)), 'поля телефона и имени должны иметь равную ширину').toBeLessThanOrEqual(12);

    await dialog.getByRole('combobox', { name: 'Номер телефона' }).fill('+7 999 123-45-67');
    await dialog.getByPlaceholder('Уточните имя звонящего').fill('Ирина тест');
    await dialog.getByRole('combobox', { name: '* Компания' }).fill('Ресторан Север');
    await page.keyboard.press('Enter');
    await dialog.getByRole('combobox', { name: '* Тип заявки' }).click();
    await page.keyboard.press('Enter');
    await dialog.getByRole('combobox', { name: 'Точка обслуживания (Bitrix24)' }).click();
    await page.keyboard.press('Enter');
    await dialog.getByPlaceholder('Опишите проблему или запрос').fill('Проверка ручного номера');
    const [request] = await Promise.all([
      page.waitForRequest((item) =>
        item.method() === 'PATCH'
        && item.url().includes('/api/telephony/tickets/ticket-created-e2e/contact'),
      ),
      dialog.getByRole('button', { name: 'Создать', exact: true }).click(),
    ]);
    const payload = JSON.parse(request.postData() || '{}') as { phone?: string; contact_name?: string };
    expect(payload.phone).toBe('+7 999 123-45-67');
    expect(payload.contact_name).toBe('Ирина тест');

    await expect(dialog).toBeHidden();
    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });

  test('показывает единое поле контакта тикета и меняет контакт без звонка', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-карточку тикета');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);
    await page.goto('/tickets/ticket-1001');

    const contactRow = page.getByRole('row', { name: /Контакт/ });
    await expect(contactRow).toContainText('Ирина кассир (+7 999 000-00-01)');
    await expect(page.getByRole('rowheader', { name: 'Имя' })).toHaveCount(0);
    await expect(page.getByRole('rowheader', { name: 'Телефон' })).toHaveCount(0);

    await contactRow.getByRole('button', { name: /Изменить/ }).click();
    const contactDialog = page.getByRole('dialog', { name: 'Контакт тикета' });
    await expect(contactDialog).toBeVisible();
    await contactDialog.getByPlaceholder('Телефон').fill('+7 999 555-44-33');
    await contactDialog.getByPlaceholder('Имя контакта').fill('Мария без звонка');

    const [saveRequest] = await Promise.all([
      page.waitForRequest((item) =>
        item.method() === 'PATCH'
        && item.url().includes('/api/telephony/tickets/ticket-1001/contact'),
      ),
      contactDialog.getByRole('button', { name: 'Сохранить' }).click(),
    ]);
    const savePayload = JSON.parse(saveRequest.postData() || '{}') as { phone?: string; contact_name?: string };
    expect(savePayload.phone).toBe('+7 999 555-44-33');
    expect(savePayload.contact_name).toBe('Мария без звонка');

    await contactRow.getByRole('button', { name: 'Отвязать' }).click();
    const [clearRequest] = await Promise.all([
      page.waitForRequest((item) =>
        item.method() === 'PATCH'
        && item.url().includes('/api/telephony/tickets/ticket-1001/contact'),
      ),
      page.locator('.ant-popconfirm-buttons').getByRole('button', { name: 'Отвязать' }).click(),
    ]);
    const clearPayload = JSON.parse(clearRequest.postData() || '{}') as { clear?: boolean };
    expect(clearPayload.clear).toBe(true);

    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });
});
