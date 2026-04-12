export type MappingDirection = 'company_to_point' | 'point_to_company';

export interface MappingDraft {
  direction: MappingDirection;
  originalCompanyId?: string;
  originalPointId?: number;
  companyId?: string;
  pointId?: number;
}

export interface MappingRowSource {
  company_id: string;
  bitrix_service_point_id?: number;
}

export interface MappedServicePointSource {
  bitrix_service_point_id?: number;
  bitrix_service_point_name?: string;
  bitrix_service_point_code?: string;
  bitrix_service_point_enabled?: boolean;
}

export const createInitialDraft = (row: MappingRowSource): MappingDraft => ({
  direction: 'company_to_point',
  originalCompanyId: row.company_id || undefined,
  originalPointId: row.bitrix_service_point_id,
  companyId: row.company_id || undefined,
  pointId: row.bitrix_service_point_id,
});

export const isDraftDirty = (draft: MappingDraft): boolean => {
  if (draft.direction === 'company_to_point') {
    return (draft.pointId ?? undefined) !== (draft.originalPointId ?? undefined);
  }
  return (draft.companyId ?? undefined) !== (draft.originalCompanyId ?? undefined);
};

export const toggleDirection = (draft: MappingDraft): MappingDraft => {
  if (draft.direction === 'company_to_point') {
    return { ...draft, direction: 'point_to_company' };
  }
  return { ...draft, direction: 'company_to_point' };
};

export const cancelDraft = (draft: MappingDraft): MappingDraft => ({
  ...draft,
  companyId: draft.originalCompanyId,
  pointId: draft.originalPointId,
});

export const formatMappedServicePointLabel = (source: MappedServicePointSource): string => {
  const parts: string[] = [];
  const name = source.bitrix_service_point_name?.trim();
  const code = source.bitrix_service_point_code?.trim();

  if (name) {
    parts.push(name);
  }
  if (code) {
    parts.push(`код 1С: ${code}`);
  }
  if (typeof source.bitrix_service_point_enabled === 'boolean') {
    parts.push(`контракт: ${source.bitrix_service_point_enabled ? 'активен' : 'нет'}`);
  }
  if (parts.length > 0) {
    return parts.join(' · ');
  }
  if (source.bitrix_service_point_id) {
    return `ID: ${source.bitrix_service_point_id}`;
  }
  return 'Точка B24 не выбрана';
};
