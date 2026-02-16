export const wrapSelectionWithTag = (tag: 'b' | 'i' | 's' | 'blockquote') => {
  switch (tag) {
    case 'b':
      document.execCommand('bold');
      break;
    case 'i':
      document.execCommand('italic');
      break;
    case 's':
      document.execCommand('strikeThrough');
      break;
    case 'blockquote':
      document.execCommand('formatBlock', false, 'blockquote');
      break;
    default:
      break;
  }
};

export const insertQuoteBlock = () => {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0) {
    wrapSelectionWithTag('blockquote');
    return;
  }
  const range = selection.getRangeAt(0);
  const selectedText = selection.toString().trim();
  if (!selectedText) {
    wrapSelectionWithTag('blockquote');
    return;
  }
  const quoteID = `quote-${Date.now()}`;
  const html = `<blockquote id="${quoteID}">${selectedText}</blockquote><p><a href="#${quoteID}">Ссылка на цитату</a></p>`;
  range.deleteContents();
  document.execCommand('insertHTML', false, html);
};

export const insertLinkAtSelection = (url: string) => {
  const normalized = String(url || '').trim();
  if (!normalized) return;
  document.execCommand('createLink', false, normalized);
};

export const insertHTMLAtSelection = (html: string) => {
  if (!html) return;
  document.execCommand('insertHTML', false, html);
};
