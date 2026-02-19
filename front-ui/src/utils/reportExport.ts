export const extractFileNameFromContentDisposition = (header?: string): string | null => {
  if (!header) return null;
  const match = /filename="?([^"]+)"?/i.exec(header);
  if (!match?.[1]) return null;
  return match[1].trim();
};

export const downloadBlob = (blob: Blob, fileName: string) => {
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = fileName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.URL.revokeObjectURL(url);
};

