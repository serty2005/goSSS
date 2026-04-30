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

export const expectVisibleControlsInsideViewport = async (locator: Locator, label: string) => {
  await expect(locator).toBeVisible();

  const outside = await locator.locator([
    'button',
    'a[href]',
    'input',
    'textarea',
    '[role="button"]',
    '[role="menuitem"]',
    '.ant-select-selector',
  ].join(',')).evaluateAll((elements) => {
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    return elements
      .map((element) => {
        const rect = element.getBoundingClientRect();
        const style = window.getComputedStyle(element);
        const visible = rect.width > 0
          && rect.height > 0
          && style.visibility !== 'hidden'
          && style.display !== 'none'
          && Number(style.opacity || '1') > 0;

        if (!visible) {
          return null;
        }

        const intersectsViewportVertically = rect.bottom > 0 && rect.top < viewportHeight;
        const isOutside = rect.left < -4
          || rect.right > viewportWidth + 4
          || (intersectsViewportVertically && rect.top < -4)
          || (intersectsViewportVertically && rect.bottom > viewportHeight + 4);

        if (!isOutside) {
          return null;
        }

        return {
          text: (element.textContent || element.getAttribute('aria-label') || element.className || element.tagName).toString().trim(),
          rect: {
            left: Math.round(rect.left),
            top: Math.round(rect.top),
            right: Math.round(rect.right),
            bottom: Math.round(rect.bottom),
          },
        };
      })
      .filter(Boolean);
  });

  expect(outside, `${label}: видимые интерактивные элементы должны помещаться во viewport`).toEqual([]);
};

export const expectNoOverlappingVisibleControls = async (locator: Locator, label: string) => {
  await expect(locator).toBeVisible();

  const overlaps = await locator.locator([
    'button',
    'a[href]',
    'input',
    'textarea',
    '[role="button"]',
    '[role="menuitem"]',
    '.ant-select-selector',
  ].join(',')).evaluateAll((elements) => {
    const controls = elements
      .map((element, index) => {
        const rect = element.getBoundingClientRect();
        const style = window.getComputedStyle(element);
        const visible = rect.width > 0
          && rect.height > 0
          && style.visibility !== 'hidden'
          && style.display !== 'none'
          && Number(style.opacity || '1') > 0;

        if (!visible) {
          return null;
        }

        return {
          index,
          text: (element.textContent || element.getAttribute('aria-label') || element.className || element.tagName).toString().trim(),
          rect,
        };
      })
      .filter((item): item is NonNullable<typeof item> => Boolean(item));

    const result: string[] = [];
    for (let i = 0; i < controls.length; i += 1) {
      for (let j = i + 1; j < controls.length; j += 1) {
        const first = controls[i];
        const second = controls[j];
        const xOverlap = Math.max(0, Math.min(first.rect.right, second.rect.right) - Math.max(first.rect.left, second.rect.left));
        const yOverlap = Math.max(0, Math.min(first.rect.bottom, second.rect.bottom) - Math.max(first.rect.top, second.rect.top));

        if (xOverlap > 4 && yOverlap > 4) {
          result.push(`${first.text || first.index} / ${second.text || second.index}`);
        }
      }
    }

    return result;
  });

  expect(overlaps, `${label}: видимые интерактивные элементы не должны перекрываться`).toEqual([]);
};
