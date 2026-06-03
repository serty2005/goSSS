export const hasRole = (roles: string[] | undefined, role: string): boolean => {
  if (!roles) return false;
  return roles.includes(role);
};

export const isAdmin = (roles: string[] | undefined): boolean => hasRole(roles, 'admin');

export const canEditCompanyBase = (roles: string[] | undefined): boolean =>
  hasRole(roles, 'admin') || hasRole(roles, 'support_specialist');

export const canEditCompanyContract = (roles: string[] | undefined): boolean =>
  hasRole(roles, 'admin');

export const canEditEquipment = (roles: string[] | undefined): boolean =>
  hasRole(roles, 'admin');

export const canManageServerActions = (roles: string[] | undefined): boolean =>
  hasRole(roles, 'admin') || hasRole(roles, 'support_specialist');
