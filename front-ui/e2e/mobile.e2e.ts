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

const openMobileNavigation = async (page: Page) => {
  await page.getByRole('button', { name: 'Переключить боковую панель' }).click();
  const drawer = page.locator('.mobile-navigation-drawer').last();
  await expect(drawer).toBeVisible();
  await expect.poll(async () => {
    const box = await drawer.boundingBox();
    return Math.round(box?.x ?? -999);
  }, { message: 'Мобильная навигация должна завершить открытие внутри viewport' }).toBeGreaterThanOrEqual(-4);
  await expect(drawer.getByText('Навигация')).toBeVisible();
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
  const drawer = await openMobileNavigation(page);
  const menu = drawer.locator('.ant-menu').first();

  if (topLevelName === itemName && itemName === 'Тикеты') {
    await menu.getByRole('menuitem', { name: topLevelName }).click();
  } else {
    await menu.getByRole('menuitem', { name: topLevelName }).first().click();
    await menu.getByRole('menuitem', { name: itemName }).last().click();
  }

  await expect(page).toHaveURL(expectedUrl);
  await expect(drawer).toBeHidden();
};

test.describe('Мобильный интерфейс ServiceDesk', () => {
  test('открывает основные разделы из мобильной навигации без overflow', async ({ page }, testInfo) => {
    test.skip(!mobileOnly(testInfo.project.name), 'Сценарий проверяет только мобильный проект');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page);

    await expect(page.locator('.ant-layout-sider-collapsed')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.app-main-header'), 'верхняя панель');
    await expectVisibleBoxInsideViewport(page.locator('.app-header-right'), 'профиль и настройки');
    await expectNoOverlappingVisibleControls(page.locator('.app-main-header'), 'верхняя панель');
    await expectDocumentHasNoHorizontalOverflow(page);

    await openMobileMenuItem(page, 'Тикеты', 'Тикеты', /\/tickets$/);
    await expect(page.locator('.tickets-mobile-workbench')).toBeVisible();
    const firstTicketCard = page.locator('.ticket-mobile-card').first();
    await expect(firstTicketCard).toBeVisible();
    await expect(page.getByText('Ресторан Север')).toBeVisible();
    await expectVisibleBoxInsideViewport(firstTicketCard, 'первая мобильная карточка тикета');
    await expectVisibleControlsInsideViewport(firstTicketCard, 'первая мобильная карточка тикета');
    await expectDocumentHasNoHorizontalOverflow(page);

    await openMobileMenuItem(page, 'Компании', 'Компании', /\/companies$/);
    await expect(page.getByRole('heading', { name: /^Компании/ })).toBeVisible();
    await expect(page.getByText('Ресторан Север')).toBeVisible();
    await expectVisibleBoxInsideViewport(page.locator('.ant-list').first(), 'список компаний');
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

    const quickPreview = page.getByLabel('Быстрый просмотр #1001');
    await expect(quickPreview).toBeVisible();
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

    const workbench = page.getByLabel('Мобильные действия по тикетам');
    await expect(workbench).toBeVisible();
    await expect(workbench.getByText('Показано: 2 из 2')).toBeVisible();
    await expect(workbench.getByRole('button', { name: 'Новая заявка' })).toBeVisible();
    await expectVisibleBoxInsideViewport(workbench, 'мобильная панель тикетов');
    await expectVisibleControlsInsideViewport(workbench, 'мобильная панель тикетов');
    await expectNoOverlappingVisibleControls(workbench, 'мобильная панель тикетов');

    const search = workbench.getByPlaceholder('Поиск по заявкам...');
    await search.fill('RDP');
    await search.press('Enter');
    await expect(workbench.getByText(/Фильтры:/)).toBeVisible();
    await expectDocumentHasNoHorizontalOverflow(page);

    await workbench.getByRole('button', { name: 'Активные' }).click();
    await workbench.getByRole('button', { name: 'Мои' }).click();
    await expectVisibleControlsInsideViewport(workbench, 'мобильные быстрые фильтры тикетов');
    await expectDocumentHasNoHorizontalOverflow(page);

    const firstCard = page.locator('.ticket-mobile-card').first();
    await expect(firstCard.getByText('#1001')).toBeVisible();
    await expect(firstCard.getByText('Ресторан Север')).toBeVisible();
    await expect(firstCard.getByText('Администратор ServiceDesk')).toBeVisible();
    await expect(firstCard.getByText('Проверить доступность фискального регистратора')).toBeVisible();
    await expectVisibleBoxInsideViewport(firstCard, 'мобильная карточка тикета');
    await expectVisibleControlsInsideViewport(firstCard, 'мобильная карточка тикета');
    await expectNoOverlappingVisibleControls(firstCard, 'мобильная карточка тикета');

    expectNoBrowserErrors(browserErrors);
  });

  test('проверяет header, профиль, оформление и Ant Design сообщения на mobile', async ({ page }, testInfo) => {
    test.skip(!mobileOnly(testInfo.project.name), 'Сценарий проверяет только мобильный проект');

    const browserErrors = collectBrowserErrors(page);

    await loginAsAdmin(page, { failNextProfileConfigPatch: true });

    const header = page.locator('.app-main-header');
    await expectVisibleBoxInsideViewport(header, 'верхняя панель');
    await expectVisibleControlsInsideViewport(header, 'верхняя панель');
    await expectNoOverlappingVisibleControls(header, 'верхняя панель');

    await page.locator('.app-header-user-trigger').click();
    const userDropdown = page.locator('.ant-dropdown').last();
    await expect(userDropdown.getByText('Профиль')).toBeVisible();
    await expect(userDropdown.getByText('Выйти')).toBeVisible();
    await expectVisibleBoxInsideViewport(userDropdown, 'меню профиля');
    await expectVisibleControlsInsideViewport(userDropdown, 'меню профиля');
    await page.keyboard.press('Escape');
    await expect(userDropdown).toBeHidden();

    await page.getByRole('button', { name: 'Открыть настройки оформления' }).click();
    const themePopover = page.locator('.ant-popover').last();
    await expect(themePopover.getByText('Оформление')).toBeVisible();
    await expect(themePopover.getByText('День')).toBeVisible();
    await expect(themePopover.getByText('Ночь')).toBeVisible();
    await expectVisibleBoxInsideViewport(themePopover, 'настройки оформления');
    await expectVisibleControlsInsideViewport(themePopover, 'настройки оформления');

    await themePopover.getByText('Ночь').click();
    const message = page.locator('#inline-message-host .ant-message-notice').last();
    await expect(message.getByText('Не удалось сохранить цветовые настройки')).toBeVisible();
    await expect.poll(async () => {
      const box = await message.boundingBox();
      return Math.round(box?.y ?? -999);
    }, { message: 'Ant Design сообщение должно завершить появление внутри viewport' }).toBeGreaterThanOrEqual(-4);
    await expectVisibleBoxInsideViewport(message, 'Ant Design сообщение');
    await expectDocumentHasNoHorizontalOverflow(page);

    expectNoBrowserErrors(
      browserErrors.filter((error) => !error.includes('Failed to load resource: the server responded with a status of 500')),
    );
  });
});
