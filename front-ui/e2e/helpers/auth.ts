import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { installMockApi } from '../fixtures/mockApi';

export const openMockedLoginPage = async (page: Page) => {
  const renderErrors: string[] = [];
  page.on('pageerror', (error) => renderErrors.push(`pageerror: ${error.message}`));
  page.on('console', (message) => {
    if (message.type() === 'error') {
      renderErrors.push(`console: ${message.text()}`);
    }
  });
  page.on('requestfailed', (request) => {
    renderErrors.push(`requestfailed: ${request.url()} ${request.failure()?.errorText || ''}`);
  });
  await installMockApi(page);
  await page.goto('/login?locale=ru');
  try {
    await expect(page.getByRole('heading', { name: 'MyHoreca XenionDesk' })).toBeVisible({ timeout: 15_000 });
  } catch (error) {
    const snapshot = await page.evaluate(() => ({
      url: window.location.href,
      title: document.title,
      body: document.body.innerText.slice(0, 500),
      rootLength: document.getElementById('root')?.innerHTML.length || 0,
    }));
    throw new Error(`Страница логина не отрендерилась: ${JSON.stringify(snapshot)}\n${renderErrors.join('\n')}\n${String(error)}`);
  }
};

export const loginAsAdmin = async (page: Page) => {
  await openMockedLoginPage(page);
  await page.getByPlaceholder('Имя пользователя').fill('admin');
  await page.getByPlaceholder('Пароль').fill('admin');
  await page.getByRole('button', { name: 'Войти' }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByText('Обзор системы')).toBeVisible();
};
