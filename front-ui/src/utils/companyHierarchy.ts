import { CompanyModel, TicketCompanyFilterItem } from '@/types/api';

const normalize = (value?: string | null) => {
  const trimmed = (value || '').trim();
  return trimmed || '';
};

export const resolveCompanyID = (company: CompanyModel) =>
  normalize(company.ID || company.id);

export const resolveCompanyTitle = (company: CompanyModel) =>
  normalize(company.Title || company.title || company.AdditionalName || company.additional_name || resolveCompanyID(company));

export const resolveCompanyParentTitle = (company: CompanyModel) =>
  normalize(company.ParentTitle || company.parent_title);

export const formatHierarchyTitle = (title: string, parentTitle?: string) => {
  const cleanTitle = normalize(title);
  const cleanParent = normalize(parentTitle);
  if (!cleanParent || cleanParent === cleanTitle) {
    return cleanTitle;
  }
  return `${cleanParent} / ${cleanTitle}`;
};

export const formatCompanyHierarchy = (company: CompanyModel) =>
  formatHierarchyTitle(resolveCompanyTitle(company), resolveCompanyParentTitle(company));

export const formatTicketFilterCompanyHierarchy = (company: TicketCompanyFilterItem) =>
  formatHierarchyTitle(company.name || company.id, company.parent_name);

export const getCompanyHierarchyParts = (title: string, parentTitle?: string) => {
  const cleanTitle = normalize(title);
  const cleanParent = normalize(parentTitle);
  if (!cleanParent || cleanParent === cleanTitle) {
    return {
      parent: '',
      child: cleanTitle,
      hasParent: false,
    };
  }
  return {
    parent: cleanParent,
    child: cleanTitle,
    hasParent: true,
  };
};
