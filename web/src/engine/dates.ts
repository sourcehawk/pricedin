// Calendar arithmetic on YYYY-MM-DD dates in UTC. Out-of-range days and
// months normalize as Go's time.Date does (March 31 less one month is
// March 3), and Date.UTC normalizes the same way, so both engines pick the
// same chart start.

function parts(date: string): [number, number, number] {
  return [Number(date.slice(0, 4)), Number(date.slice(5, 7)), Number(date.slice(8, 10))];
}

function format(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

/** addDays returns the date n calendar days after date; a negative n goes back. */
export function addDays(date: string, n: number): string {
  const [y, m, d] = parts(date);
  return format(Date.UTC(y, m - 1, d + n));
}

/** addMonths returns the date n months after date, normalized like Go's AddDate. */
export function addMonths(date: string, n: number): string {
  const [y, m, d] = parts(date);
  return format(Date.UTC(y, m - 1 + n, d));
}

const datePattern = /^(\d{4})-(\d{2})-(\d{2})$/;

/** isDate reports whether date is YYYY-MM-DD and names a day that exists, as Go's domain.ParseDate accepts it. */
export function isDate(date: string): boolean {
  const m = datePattern.exec(date);
  if (m === null) {
    return false;
  }
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const leap = (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0;
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][mo - 1];
  return days !== undefined && d >= 1 && d <= days;
}
