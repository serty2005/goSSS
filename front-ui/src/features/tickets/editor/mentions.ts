export type MentionOption = {
  id: number;
  label: string;
};

export const buildMentionHTML = (option: MentionOption) => {
  const safeName = String(option.label || '').trim() || `Пользователь #${option.id}`;
  return `<a href="#" class="etalon-user-link" data-etalon-user-id="${option.id}" data-etalon-user-name="${safeName}">@${safeName}</a>`;
};

export const extractMentionQuery = (text: string): string => {
  const match = text.match(/@([\p{L}\p{N}_\-.]*)$/u);
  if (!match) {
    return '';
  }
  return (match[1] || '').trim().toLowerCase();
};

