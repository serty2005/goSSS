import { expect, test } from '@playwright/test';
import { loginAsAdmin } from './helpers/auth';
import {
  collectBrowserErrors,
  expectDocumentHasNoHorizontalOverflow,
  expectNoBrowserErrors,
} from './helpers/pageHealth';

test.describe('Тикеты: компоновка на ноутбуках и сортировка по статусу', () => {
  test('карточка тикета читается на 1366x768 и помещается в экран по высоте', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-карточку тикета');

    const browserErrors = collectBrowserErrors(page);
    await page.setViewportSize({ width: 1366, height: 768 });
    await loginAsAdmin(page);
    await page.goto('/tickets/ticket-1001');

    const serviceCard = page.locator('.ticket-overview-service-card');
    await expect(serviceCard).toBeVisible();

    // Значения служебной информации не должны ломаться по символам в узкую колонку.
    const companyLink = serviceCard.getByRole('link', { name: 'Ресторан Север' }).first();
    await expect(companyLink).toBeVisible();
    const companyBox = await companyLink.boundingBox();
    expect(companyBox?.height || 0, 'название компании должно идти в одну строку').toBeLessThan(40);
    expect(companyBox?.width || 0, 'название компании не должно быть сжато').toBeGreaterThan(80);

    const valueCells = serviceCard.locator('.ant-descriptions-item-content');
    const cellCount = await valueCells.count();
    expect(cellCount).toBeGreaterThan(0);
    for (let index = 0; index < cellCount; index += 1) {
      const box = await valueCells.nth(index).boundingBox();
      expect(box?.width || 0, `ячейка значения №${index + 1} слишком узкая`).toBeGreaterThanOrEqual(150);
    }

    // Страница не выходит за экран по высоте: прокручиваются панели, а не документ.
    const pageBox = await page.locator('.ticket-details-page').boundingBox();
    expect(pageBox).not.toBeNull();
    expect((pageBox?.y || 0) + (pageBox?.height || 0), 'страница тикета должна помещаться в viewport').toBeLessThanOrEqual(768);

    // Вид служебной информации и раскладка карточек оборудования переключаются одновременно.
    await expect(serviceCard.locator('.ticket-service-descriptions')).toHaveClass(/ticket-service-descriptions--vertical/);
    await expect(page.locator('.ticket-overview-equipment-grid')).not.toHaveClass(/ticket-overview-equipment-grid--two-columns/);

    // Карточки оборудования занимают колонку целиком, поля внутри не ломаются по буквам.
    const firstEquipmentCard = page.locator('.ticket-overview-side-card .ticket-equipment-card').first();
    await expect(firstEquipmentCard).toBeVisible();
    const versionButton = firstEquipmentCard.getByRole('button', { name: /Версия/ });
    const versionBox = await versionButton.boundingBox();
    expect(versionBox?.width || 0, 'кнопка «Версия» не должна быть узкой').toBeGreaterThanOrEqual(120);

    // Комментарии доступны без потери вкладок: основная колонка прокручивается.
    const commentsTab = page.locator('.ticket-overview-comments-card').getByRole('tab', { name: /Комментарии/ });
    await commentsTab.scrollIntoViewIfNeeded();
    await expect(commentsTab).toBeVisible();

    await expectDocumentHasNoHorizontalOverflow(page);
    expectNoBrowserErrors(browserErrors);
  });

  test('клик по колонке «Статус» включает серверную сортировку и группы по статусам', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-таблицу тикетов');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);

    const requestedSorts: Array<string | null> = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname === '/api/tickets') {
        requestedSorts.push(url.searchParams.get('sort_by'));
      }
    });

    await page.goto('/tickets');
    const statusHeader = page.locator('th', { hasText: 'Статус' }).first();
    await expect(statusHeader).toBeVisible();
    await expect(page.locator('.ticket-status-group-header')).toHaveCount(0);

    await statusHeader.click();
    await expect.poll(() => requestedSorts.includes('status:asc')).toBe(true);
    const groups = page.locator('.ticket-status-group-header');
    await expect(groups.first()).toBeVisible();
    // Счетчик группы берется с сервера и складывает устаревший статус registered с актуальным new.
    await expect(groups.filter({ hasText: 'Новая' })).toContainText('6');
    await expect(groups.filter({ hasText: 'В работе' })).toContainText('3');

    await statusHeader.click();
    await expect.poll(() => requestedSorts.includes('status:desc')).toBe(true);

    await statusHeader.click();
    await expect(page.locator('.ticket-status-group-header')).toHaveCount(0);

    expectNoBrowserErrors(browserErrors);
  });

  test('группу статуса можно свернуть кликом: заявки не загружаются, счетчик остается', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name.includes('mobile'), 'Сценарий проверяет desktop-таблицу тикетов');

    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);

    const excludedStatuses: Array<string | null> = [];
    page.on('request', (request) => {
      const url = new URL(request.url());
      if (url.pathname === '/api/tickets') {
        excludedStatuses.push(url.searchParams.get('exclude_status'));
      }
    });

    await page.goto('/tickets?table_sort=status:asc');
    const newGroup = page.locator('.ticket-status-group-header', { hasText: 'Новая' });
    await expect(newGroup).toHaveAttribute('aria-expanded', 'true');
    await expect(page.getByRole('link', { name: '#1001' })).toBeVisible();

    await newGroup.click();
    await expect.poll(() => excludedStatuses.includes('new')).toBe(true);
    await expect(newGroup).toHaveAttribute('aria-expanded', 'false');
    await expect(newGroup).toContainText('6');
    await expect(page.getByRole('link', { name: '#1001' })).toHaveCount(0);

    await newGroup.click();
    await expect(newGroup).toHaveAttribute('aria-expanded', 'true');
    await expect(page.getByRole('link', { name: '#1001' })).toBeVisible();

    expectNoBrowserErrors(browserErrors);
  });

  test('в базе знаний есть источник «Файлы» и добавление материала без привязки', async ({ page }) => {
    const browserErrors = collectBrowserErrors(page);
    await loginAsAdmin(page);

    await page.goto('/info?source=files');
    await expect(page.locator('.ant-select', { hasText: 'Файлы' })).toBeVisible();

    const fileLink = page.getByRole('link', { name: 'схема.pdf', exact: true });
    await expect(fileLink).toBeVisible();
    await expect(fileLink).toHaveAttribute('href', /material-files\/material-file-1\/file-1\/schema\.pdf$/);
    await expect(page.getByText('Без привязки', { exact: true })).toBeVisible();
    await expect(page.getByText('Заметка без файлов')).toHaveCount(0);

    await page.getByRole('button', { name: /Добавить материал или файл/ }).click();
    const dialog = page.getByRole('dialog', { name: 'Новый материал' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText('Привязки (необязательно)')).toBeVisible();
    await expect(dialog.getByRole('button', { name: /Прикрепить файлы/ })).toBeEnabled();

    expectNoBrowserErrors(browserErrors);
  });
});
