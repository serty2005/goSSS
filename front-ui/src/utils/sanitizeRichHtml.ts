const allowedTags = new Set([
  'A', 'B', 'I', 'S', 'STRONG', 'EM', 'U', 'P', 'BR', 'DIV', 'SPAN', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'IMG',
]);

const sanitizeURL = (value: string) => {
  const candidate = String(value || '').trim();
  if (!candidate) return '';
  if (/^javascript:/i.test(candidate)) return '';
  const normalized = candidate
    .replace(/^\/static\//i, '/api/static/')
    .replace(/^static\//i, '/api/static/');
  return normalized;
};

export const sanitizeRichHtml = (value?: string) => {
  const raw = String(value || '').trim();
  if (!raw) return '';
  const parser = new DOMParser();
  const doc = parser.parseFromString(raw, 'text/html');

  const walk = (element: Element) => {
    const children = Array.from(element.children);
    children.forEach((child) => {
      if (!allowedTags.has(child.tagName)) {
        const fragment = doc.createDocumentFragment();
        while (child.firstChild) {
          fragment.appendChild(child.firstChild);
        }
        child.replaceWith(fragment);
        return;
      }

      Array.from(child.attributes).forEach((attr) => {
        const name = attr.name.toLowerCase();
        const isAllowedData = name.startsWith('data-') && child.tagName === 'A' && child.classList.contains('etalon-user-link');
        const allowed = ['href', 'src', 'alt', 'class', 'target', 'rel'].includes(name) || isAllowedData;
        if (!allowed) {
          child.removeAttribute(attr.name);
        }
      });

      if (child.tagName === 'A') {
        const href = sanitizeURL(child.getAttribute('href') || '');
        if (href) {
          child.setAttribute('href', href);
          child.setAttribute('target', '_blank');
          child.setAttribute('rel', 'noreferrer');
        } else if (!child.classList.contains('etalon-user-link')) {
          child.removeAttribute('href');
        }
      }
      if (child.tagName === 'IMG') {
        const src = sanitizeURL(child.getAttribute('src') || '');
        if (!src) {
          child.remove();
          return;
        }
        child.setAttribute('src', src);
      }

      walk(child);
    });
  };

  walk(doc.body);
  return doc.body.innerHTML;
};
