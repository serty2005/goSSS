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
