import type { Locator, Page } from '@playwright/test';
import { expect } from '@playwright/test';

export const collectBrowserErrors = (page: Page) => {
  const errors: string[] = [];

  page.on('pageerror', (error) => {
    errors.push(error.message);
  });

  page.on('console', (message) => {
    if (message.type() === 'error') {
      const text = message.text();
      if (text.startsWith('Warning: [antd:')) {
        return;
      }
      errors.push(text);
    }
  });

  return errors;
};

export const expectNoBrowserErrors = (errors: string[]) => {
  expect(errors, 'В браузерной консоли не должно быть ошибок').toEqual([]);
};

export const expectDocumentHasNoHorizontalOverflow = async (page: Page) => {
  const overflow = await page.evaluate(() => {
    const root = document.documentElement;
    return Math.max(0, root.scrollWidth - root.clientWidth);
  });

  expect(overflow, 'Документ не должен создавать горизонтальный скролл').toBeLessThanOrEqual(1);
};

export const expectVisibleBoxInsideViewport = async (locator: Locator, label: string) => {
  await expect(locator).toBeVisible();

  const box = await locator.boundingBox();
  expect(box, `${label}: элемент должен иметь измеримый bounding box`).not.toBeNull();
  if (!box) {
    return;
  }

  const viewport = locator.page().viewportSize();
  expect(viewport, `${label}: viewport должен быть доступен`).not.toBeNull();
  if (!viewport) {
    return;
  }

  expect(box.x, `${label}: левая граница внутри viewport`).toBeGreaterThanOrEqual(-4);
  expect(box.y, `${label}: верхняя граница внутри viewport`).toBeGreaterThanOrEqual(-4);
  expect(box.x + box.width, `${label}: правая граница внутри viewport`).toBeLessThanOrEqual(viewport.width + 4);
  expect(box.y + box.height, `${label}: нижняя граница внутри viewport`).toBeLessThanOrEqual(viewport.height + 4);
};
