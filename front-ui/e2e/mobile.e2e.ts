import { expect, test, type Page } from '@playwright/test';
import { loginAsAdmin } from './helpers/auth';
import {
  collectBrowserErrors,
  expectDocumentHasNoHorizontalOverflow,
  expectNoBrowserErrors,
  expectNoOverlappingVisibleControls,
  expectVisibleBoxInsideViewport,
  expectVisibleControlsInsideViewport,
} from './helpers/pageHealth';

const mobileOnly = (projectName: string) => projectName.includes('mobile');

const openMobileNavigation = async (page: Page, groupName: string) => {
  await page.getByRole('button', { name: `Открыть раздел ${groupName}` }).click();
  const drawer = page.locator('.mobile-navigation-drawer').last();
  await expect(drawer).toBeVisible();
  await expect.poll(async () => {
    const box = await drawer.boundingBox();
    return Math.round(box?.x ?? -999);
  }, { message: 'Мобильная навигация должна завершить открытие внутри viewport' }).toBeGreaterThanOrEqual(-4);
  await expect.poll(async () => {
    const box = await drawer.boundingBox();
    const viewport = page.viewportSize();
    return Math.round((box?.y ?? 999) + (box?.height ?? 0) - (viewport?.height ?? 0));
  }, { message: 'Мобильная навигация снизу должна помещаться во viewport' }).toBeLessThanOrEqual(4);
  await expect(drawer.locator('.ant-drawer-title')).toHaveText(groupName);
  await expectVisibleBoxInsideViewport(drawer, 'мобильная навигация');
  await expectVisibleControlsInsideViewport(drawer, 'мобильная навигация');
  await expectNoOverlappingVisibleControls(drawer, 'мобильная навигация');
  return drawer;
};

const openMobileMenuItem = async (
  page: Page,
  topLevelName: string,
  itemName: string,
  expectedUrl: RegExp,
) => {
  if (topLevelName === itemName && itemName === 'Тикеты') {
    await page.getByRole('button', { name: topLevelName }).click();
    await expect(page).toHaveURL(expectedUrl);
    return;
  } else {
    const drawer = await openMobileNavigation(page, topLevelName);
    const menu = drawer.locator('.ant-menu').first();
    await menu.getByRole('menuitem', { name: itemName }).last().click();
    await expect(page).toHaveURL(expectedUrl);
    await expect(drawer).toBeHidden();
  }
};

test.describe('Мобильный интерфейс ServiceDesk', () => {
  test('открывает основные разделы из мобильной навигации без overflow', async ({ page }, testInfo) => {
    test.skip(!mobileOnly(testInfo.project.name), 'Сценарий проверяет только мобильный проект');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);

    await expect(page.locator('.ant-layout-sider')).toBeHidden();
    await expectVisibleBoxInsideViewport(page.locator('.app-main-header'), 'верхняя панель');
    await expect(page.locator('.app-header-right')).toBeHidden();
    await expect(page.locator('.app-main-header').getByPlaceholder('Поиск по IP, серийному номеру, имени...')).toBeVisible();
    await expectNoOverlappingVisibleControls(page.locator('.app-main-header'), 'верхняя панель');
    await expectVisibleBoxInsideViewport(page.locator('.app-mobile-action-bar'), 'нижняя панель действий');
    await expectVisibleControlsInsideViewport(page.locator('.app-mobile-action-bar'), 'нижняя панель действий');
    await expectNoOverlappingVisibleControls(page.locator('.app-mobile-action-bar'), 'нижняя панель действий');
    await expectDocumentHasNoHorizontalOverflow(page);

    await openMobileMenuItem(page, 'Тикеты', 'Тикеты', /\/tickets$/);
    await expect(page.locator('.tickets-filter-panel')).toHaveCount(0);
    await expect(page.getByLabel('Сводка списка заявок')).toBeVisible();
    const firstTicketCard = page.locator('.ticket-mobile-card').first();
    await expect(firstTicketCard).toBeVisible();
    await expect(page.getByText('Ресторан Север')).toBeVisible();
    await firstTicketCard.scrollIntoViewIfNeeded();
    await expectVisibleBoxInsideViewport(firstTicketCard, 'первая мобильная карточка тикета');
    await expectVisibleControlsInsideViewport(firstTicketCard, 'первая мобильная карточка тикета');
    await expectDocumentHasNoHorizontalOverflow(page);

    await openMobileMenuItem(page, 'Компании', 'Компании', /\/companies$/);
    await expect(page.getByRole('heading', { name: /^Компании/ })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Ресторан Север' })).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.ant-table-wrapper').first(), 'таблица компаний');
    await expectDocumentHasNoHorizontalOverflow(page);

    await openMobileMenuItem(page, 'Оборудование', 'Серверы', /\/servers$/);
    await expect(page.getByRole('heading', { name: /^Серверы/ })).toBeVisible();
    await expect(page.getByText('srv-rest-sever')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.ant-table-wrapper').first(), 'таблица серверов');
    await expectDocumentHasNoHorizontalOverflow(page);

    await openMobileMenuItem(page, 'Администрирование', 'Пользователи', /\/admin\/users$/);
    await expect(page.getByRole('heading', { name: 'Сотрудники' })).toBeVisible();
    await expect(page.getByText('Мария оператор')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.ant-table-wrapper').first(), 'таблица сотрудников');
    await expectDocumentHasNoHorizontalOverflow(page);

    expectNoBrowserErrors(browserErrors);
  });

  test('показывает быстрый просмотр тикета на mobile без выхода за viewport', async ({ page }, testInfo) => {
    test.skip(!mobileOnly(testInfo.project.name), 'Сценарий проверяет только мобильный проект');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);
    await page.goto('/tickets');

    await expect(page.locator('.ticket-mobile-card').first()).toBeVisible();
    await page.getByRole('button', { name: /Быстрый просмотр тикета #1001/ }).click();

    const quickPreview = page.getByRole('dialog', { name: /Быстрый просмотр #1001/ });
    await expect(quickPreview).toBeVisible();
    await expect.poll(async () => {
      const box = await quickPreview.boundingBox();
      const viewport = page.viewportSize();
      if (!box || !viewport) return 999;
      return Math.round(box.y + box.height - viewport.height);
    }, { message: 'Быстрый просмотр должен завершить открытие внутри viewport' }).toBeLessThanOrEqual(4);
    await expectVisibleBoxInsideViewport(quickPreview, 'быстрый просмотр тикета');
    await expect(quickPreview.getByText('Проверить доступность фискального регистратора')).toBeVisible();
    await expect(quickPreview.getByText('Подключения')).toBeVisible();
    await expect(quickPreview.getByText('srv-rest-sever')).toBeVisible();
    await expectVisibleControlsInsideViewport(quickPreview, 'быстрый просмотр тикета');
    await expectNoOverlappingVisibleControls(quickPreview, 'быстрый просмотр тикета');
    await expectDocumentHasNoHorizontalOverflow(page);

    await page.keyboard.press('Escape');
    await expect(quickPreview).toBeHidden();
    expectNoBrowserErrors(browserErrors);
  });

  test('показывает мобильную рабочую панель тикетов с поиском, фильтрами и карточками', async ({ page }, testInfo) => {
    test.skip(!mobileOnly(testInfo.project.name), 'Сценарий проверяет только мобильный проект');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);
    await page.goto('/tickets');

    const header = page.locator('.app-main-header');
    await expect(header.getByPlaceholder('Поиск по заявкам...')).toBeVisible();
    await expect(header.getByRole('button', { name: 'Открыть настройки и профиль' })).toBeVisible();
    await expectVisibleBoxInsideViewport(header, 'мобильный header с поиском');
    await expectVisibleControlsInsideViewport(header, 'мобильный header с поиском');
    await expectNoOverlappingVisibleControls(header, 'мобильный header с поиском');

    const workbench = page.getByLabel('Сводка списка заявок');
    await expect(workbench).toBeVisible();
    await expect(workbench.getByText('Показано: 2 из 2')).toBeVisible();
    await expect(workbench.getByRole('button', { name: 'Новая заявка' })).toBeVisible();
    await expectVisibleBoxInsideViewport(workbench, 'сводка списка тикетов');
    await expectVisibleControlsInsideViewport(workbench, 'сводка списка тикетов');
    await expectNoOverlappingVisibleControls(workbench, 'сводка списка тикетов');

    const search = header.getByPlaceholder('Поиск по заявкам...');
    await search.fill('RDP');
    await search.press('Enter');
    await expect(workbench.getByText(/Фильтры:/)).toBeVisible();
    await expectDocumentHasNoHorizontalOverflow(page);

    await header.getByRole('button', { name: 'Открыть настройки и профиль' }).click();
    const settingsPopover = page.locator('.ant-popover').last();
    await expect(settingsPopover.getByText('Режим списка')).toBeVisible();
    await expect(settingsPopover.getByText('Статусы')).toBeVisible();
    await expect(settingsPopover.getByText('Сотрудники')).toBeVisible();
    await expect(settingsPopover.getByText('Компания')).toBeVisible();
    await expect(settingsPopover.getByText('Мои')).toBeVisible();
    await expect(settingsPopover.getByText('Профиль')).toBeVisible();
    await expect(settingsPopover.getByText('Выйти')).toBeVisible();
    await expectVisibleBoxInsideViewport(settingsPopover, 'меню настроек тикетов');
    await expectVisibleControlsInsideViewport(settingsPopover, 'меню настроек тикетов');

    await settingsPopover.getByRole('checkbox', { name: 'Активные' }).check();
    await settingsPopover
      .locator('.ant-select')
      .filter({ hasText: 'Компания' })
      .locator('input[role="combobox"]')
      .first()
      .click({ force: true });
    const companyDropdown = page.locator('.ant-select-dropdown').last();
    await expect(companyDropdown.getByText('Ресторан Север')).toBeVisible();
    await expectVisibleBoxInsideViewport(companyDropdown, 'выпадающий список компаний');
    await page.keyboard.press('Escape');
    await page.keyboard.press('Escape');
    await expectVisibleControlsInsideViewport(workbench, 'мобильная сводка тикетов');
    await expectDocumentHasNoHorizontalOverflow(page);

    const firstCard = page.locator('.ticket-mobile-card').first();
    await expect(firstCard.getByText('#1001')).toBeVisible();
    await expect(firstCard.getByText('Ресторан Север')).toBeVisible();
    await expect(firstCard.getByText('Администратор ServiceDesk')).toBeVisible();
    await expect(firstCard.getByText('Проверить доступность фискального регистратора')).toBeVisible();
    await firstCard.scrollIntoViewIfNeeded();
    await expectVisibleBoxInsideViewport(firstCard, 'мобильная карточка тикета');
    await expectVisibleControlsInsideViewport(firstCard, 'мобильная карточка тикета');
    await expectNoOverlappingVisibleControls(firstCard, 'мобильная карточка тикета');

    expectNoBrowserErrors(browserErrors);
  });

  test('проверяет header, профиль и оформление на mobile', async ({ page }, testInfo) => {
    test.skip(!mobileOnly(testInfo.project.name), 'Сценарий проверяет только мобильный проект');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);

    const header = page.locator('.app-main-header');
    await expectVisibleBoxInsideViewport(header, 'верхняя панель');
    await expectVisibleControlsInsideViewport(header, 'верхняя панель');
    await expectNoOverlappingVisibleControls(header, 'верхняя панель');

    await expect(page.locator('.app-header-right')).toBeHidden();
    await expect(page.locator('.app-main-header').getByPlaceholder('Поиск по IP, серийному номеру, имени...')).toBeVisible();

    const mobileActions = page.locator('.app-mobile-action-bar');
    await expectVisibleBoxInsideViewport(mobileActions, 'нижняя панель действий');
    await expectVisibleControlsInsideViewport(mobileActions, 'нижняя панель действий');
    await expectNoOverlappingVisibleControls(mobileActions, 'нижняя панель действий');

    await page.getByRole('button', { name: 'Открыть настройки и профиль' }).click();
    const userDropdown = page.locator('.ant-popover').last();
    await expect(userDropdown.getByText('Профиль')).toBeVisible();
    await expect(userDropdown.getByText('Выйти')).toBeVisible();
    await expectVisibleBoxInsideViewport(userDropdown, 'меню профиля');
    await expectVisibleControlsInsideViewport(userDropdown, 'меню профиля');
    await page.keyboard.press('Escape');
    await expect(userDropdown).toBeHidden();

    await expectDocumentHasNoHorizontalOverflow(page);

    expectNoBrowserErrors(browserErrors);
  });
});
