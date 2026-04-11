import { describe, expect, it } from 'vitest';
import { cancelDraft, createInitialDraft, formatMappedServicePointLabel, isDraftDirty, toggleDirection } from '../src/pages/companies/companyBitrixMappingState';

describe('companyBitrixMappingState', () => {
  it('определяет dirty в режиме company_to_point', () => {
    const draft = createInitialDraft({ company_id: 'c1', bitrix_service_point_id: 10 });
    expect(isDraftDirty(draft)).toBe(false);

    const changed = { ...draft, pointId: 11 };
    expect(isDraftDirty(changed)).toBe(true);
  });

  it('поддерживает toggle направления и dirty по компании', () => {
    const draft = createInitialDraft({ company_id: 'c1', bitrix_service_point_id: 10 });
    const toggled = toggleDirection(draft);

    expect(toggled.direction).toBe('point_to_company');
    expect(isDraftDirty(toggled)).toBe(false);

    const changed = { ...toggled, companyId: 'c2' };
    expect(isDraftDirty(changed)).toBe(true);
  });

  it('cancel возвращает исходные значения', () => {
    const draft = createInitialDraft({ company_id: 'c1', bitrix_service_point_id: 10 });
    const changed = { ...draft, pointId: undefined };

    const canceled = cancelDraft(changed);
    expect(canceled.companyId).toBe('c1');
    expect(canceled.pointId).toBe(10);
    expect(isDraftDirty(canceled)).toBe(false);
  });

  it('формирует понятную подпись для сохранённой точки Bitrix24', () => {
    expect(formatMappedServicePointLabel({
      bitrix_service_point_id: 42,
      bitrix_service_point_name: 'Кафе на Пушкина',
      bitrix_service_point_code: 'RU0042',
      bitrix_service_point_enabled: true,
    })).toBe('Кафе на Пушкина · код 1С: RU0042 · контракт: активен');
  });

  it('использует id как fallback для точки без compute-данных', () => {
    expect(formatMappedServicePointLabel({
      bitrix_service_point_id: 77,
    })).toBe('ID: 77');
  });
});
