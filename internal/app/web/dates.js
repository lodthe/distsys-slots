const fmt = (value, options) =>
  new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', ...options }).format(new Date(value));
const time = (value) => fmt(value, { hour: '2-digit', minute: '2-digit' });
const date = (value) => fmt(value, { day: 'numeric', month: 'long', weekday: 'short' });
const isoDay = (value = new Date()) =>
  new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Moscow',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(value);
function minutes(value) {
  const [hours, mins] = value.split(':').map(Number);
  return hours * 60 + mins;
}
function clock(value) {
  const n = ((value % 1440) + 1440) % 1440;
  return String(Math.floor(n / 60)).padStart(2, '0') + ':' + String(n % 60).padStart(2, '0');
}

export { fmt, time, date, isoDay, minutes, clock };
