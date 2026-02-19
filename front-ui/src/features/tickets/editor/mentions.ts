export type MentionOption = {
  id: number;
  label: string;
};

const escapeHtml = (value: string): string => value
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;')
  .replace(/'/g, '&#39;');

export const buildMentionHTML = (option: MentionOption) => {
  const rawName = String(option.label || '').trim() || `Пользователь #${option.id}`;
  const safeName = escapeHtml(rawName);
  return `<a href="#" class="etalon-user-link" data-etalon-user-id="${option.id}" data-etalon-user-name="${safeName}">@${safeName}</a>&nbsp;`;
};

export const extractMentionQuery = (text: string): string => {
  const match = text.match(/(?:^|[\s(])@([\p{L}\p{N}_\-.]*)$/u);
  if (!match) {
    return '';
  }
  return (match[1] || '').trim().toLowerCase();
};
