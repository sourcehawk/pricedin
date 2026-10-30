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
