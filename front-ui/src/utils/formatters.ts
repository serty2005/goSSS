import dayjs from 'dayjs';

export const formatRnm = (rnm?: string): string => {
  if (!rnm) return '';
  // Разбиваем по 4 цифры: 0000 1111 2222 3333
  return rnm.replace(/\D/g, '').replace(/(\d{4})(?=\d)/g, '$1 ').trim();
};

export const cleanWebUrl = (url?: string): string => {
  if (!url) return '';
  // Убираем протокол если есть, убираем порт и все после него
  // co-mirine-co.iikoweb.ru:8080 -> co-mirine-co.iikoweb.ru
  let clean = url.replace(/https?:\/\//, '');
  clean = clean.split(':')[0];
  return clean;
};

export const formatServerEdition = (edition?: string): string => {
  if (!edition) return '';
  const lower = edition.toLowerCase();
  if (lower === 'default') return 'RMS';
  if (lower === 'chain') return 'Chain';
  return edition;
};

export const formatDate = (date?: string): string => {
  if (!date) return '-';
  return dayjs(date).format('DD.MM.YYYY HH:mm');
};