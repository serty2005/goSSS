export const hasEditorContent = (value?: string): boolean => {
  const source = String(value || '');
  if (!source.trim()) return false;

  if (/<img\b[^>]*>/i.test(source)) {
    return true;
  }

  const text = source
    .replace(/<br\s*\/?\s*>/gi, '\n')
    .replace(/<[^>]+>/g, ' ')
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .trim();

  return text.length > 0;
};
