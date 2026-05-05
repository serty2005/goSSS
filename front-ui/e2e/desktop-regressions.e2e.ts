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

    const numberLink = page.locator('a.ticket-number-cell-link', { hasText: '#1001' }).first();
    const numberCell = numberLink.locator('xpath=ancestor::td[1]');
    const numberCellBox = await numberCell.boundingBox();
    const numberLinkBox = await numberLink.boundingBox();
    expect(numberCellBox).not.toBeNull();
    expect(numberLinkBox).not.toBeNull();
    expect(numberLinkBox?.width || 0, 'ссылка номера должна занимать почти всю ячейку').toBeGreaterThan((numberCellBox?.width || 0) - 24);

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

  test('показывает сетевую инфраструктуру компании в правом блоке', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-компоновку компании');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);
    await page.goto('/companies/company-1');

    const networkBlock = page.locator('.company-network-aside-card');
    await expect(networkBlock).toContainText('Инфраструктура сети');
    await expect(networkBlock.getByText('Родитель')).toBeVisible();
    await expect(networkBlock.getByText('Ресторан Север Бар')).toBeVisible();
    await expect(networkBlock.getByText('Ресторан Север Доставка')).toBeVisible();
    await expect(networkBlock.getByText('Ресторан Север Склад')).toBeVisible();
    await expect(page.getByRole('tab', { name: 'Инфраструктура' })).toHaveCount(0);
    await expect(networkBlock.getByText('Текущая')).toHaveCount(0);

    const summaryBox = await page.locator('.company-summary-card').boundingBox();
    const networkBox = await networkBlock.boundingBox();
    expect(summaryBox).not.toBeNull();
    expect(networkBox).not.toBeNull();
    expect(Math.abs((summaryBox?.y || 0) - (networkBox?.y || 0)), 'инфраструктура должна начинаться на уровне служебной информации').toBeLessThanOrEqual(4);

    const rootBox = await networkBlock.getByText('Ресторан Север', { exact: true }).first().boundingBox();
    const childBox = await networkBlock.getByText('Ресторан Север Бар').first().boundingBox();
    expect(rootBox).not.toBeNull();
    expect(childBox).not.toBeNull();
    expect((rootBox?.y || 0), 'родительская компания должна быть выше дочерних').toBeLessThan(childBox?.y || 0);

    await page.evaluate(() => {
      (window as typeof window & { __companyNavigationMarker?: string }).__companyNavigationMarker = 'spa';
    });
    await networkBlock.getByRole('link', { name: 'Ресторан Север Бар' }).click();
    await expect(page).toHaveURL(/\/companies\/company-child-1$/);
    await expect(page.locator('.company-summary-card')).toContainText('Ресторан Север Бар');
    await expect.poll(() => page.evaluate(() => (
      window as typeof window & { __companyNavigationMarker?: string }
    ).__companyNavigationMarker)).toBe('spa');

    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });

  test('объединяет оборудование тикета с подключениями и показывает последние тикеты компании', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-карточку тикета');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);
    await page.goto('/tickets/ticket-1001');
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText: async () => undefined },
        configurable: true,
      });
    });

    const sideCard = page.locator('.ticket-overview-side-card');
    await expect(sideCard.getByRole('tab', { name: 'Оборудование' })).toBeVisible();
    await expect(sideCard.getByRole('tab', { name: 'Тикеты' })).toBeVisible();
    await expect(sideCard.getByRole('tab', { name: 'Звонки' })).toBeVisible();
    await expect(sideCard.getByRole('tab', { name: 'Подключения' })).toHaveCount(0);

    await expect(sideCard.getByText('Серверы родительской компании')).toHaveCount(0);
    await expect(sideCard.getByText('srv-rest-sever')).toBeVisible();
    await expect(sideCard.getByText('Сервер', { exact: true })).toHaveCount(0);
    await expect(sideCard.getByRole('link', { name: 'SyrveApp' })).toHaveAttribute('href', 'https://demo.syrve.app/');
    await expect(sideCard.getByText('IP:порт')).toBeVisible();
    await expect(sideCard.getByText('CRMid')).toHaveCount(0);
    await expect(sideCard.getByRole('button', { name: /UID UID-SRV-001/ })).toBeVisible();
    await expect(sideCard.getByText('Касса 1')).toBeVisible();
    await expect(sideCard.getByText('АТОЛ 55Ф')).toBeVisible();
    await expect(sideCard.getByText('АТОЛ UUID')).toBeVisible();
    await expect(sideCard.getByText('Фастфуд 4 (Пицца 2)')).toBeVisible();
    await expect(sideCard.getByText('ШТРИХ-М-01Ф')).toBeVisible();
    const equipmentGrid = sideCard.locator('.ticket-overview-equipment-grid');
    await expect.poll(() => equipmentGrid.evaluate((node) => node instanceof HTMLElement && !node.innerText.includes('Тип:'))).toBe(true);
    await expect.poll(() => equipmentGrid.evaluate((node) => node instanceof HTMLElement && !node.innerText.includes('ID агента'))).toBe(true);
    await expect(sideCard.getByRole('button', { name: 'agent' })).toHaveCount(2);
    await expect(sideCard.getByText('Оборудование без ID агента')).toHaveCount(0);

    const equipmentCards = equipmentGrid.locator('> .ticket-equipment-card');
    await expect(equipmentCards).toHaveCount(5);
    const firstEquipmentCardBox = await equipmentCards.nth(0).boundingBox();
    const secondEquipmentCardBox = await equipmentCards.nth(1).boundingBox();
    expect(firstEquipmentCardBox).not.toBeNull();
    expect(secondEquipmentCardBox).not.toBeNull();
    expect(Math.abs((firstEquipmentCardBox?.y || 0) - (secondEquipmentCardBox?.y || 0)), 'карточки оборудования должны идти в две колонки').toBeLessThanOrEqual(4);
    expect((secondEquipmentCardBox?.x || 0), 'вторая карточка должна быть в правой колонке').toBeGreaterThan(firstEquipmentCardBox?.x || 0);

    await sideCard.getByRole('button', { name: /IP:порт/ }).click();
    await expect(page.locator('.ant-message-notice-content').last()).toContainText('Значение скопировано');

    const serverCard = equipmentCards.nth(0);
    const versionBox = await serverCard.getByRole('button', { name: /Версия/ }).boundingBox();
    const uidBox = await serverCard.getByRole('button', { name: /UID/ }).boundingBox();
    expect(versionBox).not.toBeNull();
    expect(uidBox).not.toBeNull();
    expect(Math.abs((versionBox?.y || 0) - (uidBox?.y || 0)), 'компактные кнопки сервера должны быть в одном ряду').toBeLessThanOrEqual(4);
    expect((versionBox?.width || 0), 'компактная кнопка сервера должна быть уже IP').toBeLessThan((await serverCard.getByRole('button', { name: /IP:порт/ }).boundingBox())?.width || 0);

    const workstationCard = equipmentCards.nth(3);
    const anydeskBox = await workstationCard.getByRole('button', { name: /AnyDesk/ }).boundingBox();
    const teamviewerBox = await workstationCard.getByRole('button', { name: /TeamViewer/ }).boundingBox();
    expect(anydeskBox).not.toBeNull();
    expect(teamviewerBox).not.toBeNull();
    expect(Math.abs((anydeskBox?.y || 0) - (teamviewerBox?.y || 0)), 'ID подключений станции должны быть в две колонки').toBeLessThanOrEqual(4);

    const [fallbackRequest] = await Promise.all([
      page.waitForRequest((request) =>
        request.url().includes('/api/agent-observations')
        && request.url().includes('workstation_id=workstation-uuid-agent'),
      ),
      sideCard.getByRole('button', { name: 'agent' }).first().click(),
    ]);
    expect(fallbackRequest.url()).toContain('workstation_id=workstation-uuid-agent');
    const observationDialog = page.getByRole('dialog', { name: /Последнее наблюдение агента/ });
    await expect(observationDialog).toBeVisible();
    await expect(observationDialog).toContainText('#9001');
    await observationDialog.getByRole('button', { name: 'Закрыть' }).click();

    await sideCard.getByRole('tab', { name: 'Тикеты' }).click();
    await expect(sideCard.getByText('Период создания')).toBeVisible();
    await expect(sideCard.getByText('Период закрытия (Решено)')).toBeVisible();
    await expect(sideCard.getByRole('columnheader', { name: 'Компания' })).toHaveCount(0);
    await expect(sideCard.getByRole('link', { name: '#1003' })).toBeVisible();
    await expect(sideCard.getByText('Повторная диагностика оборудования.')).toBeVisible();
    await expect(sideCard.getByText('Показано: 1 из 1')).toBeVisible();
    await expect(sideCard.getByRole('link', { name: '#1001' })).toHaveCount(0);

    await sideCard.getByRole('tab', { name: 'Звонки' }).click();
    await expect(sideCard.getByText('Звонки по тикету пока не привязаны')).toBeVisible();

    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });
});
