// Helpers for the guided search: they read and change one field's term in
// a query's text, so the text stays the search and anything typed by hand
// is kept. Only top-level terms are touched: words inside parentheses, and
// OR groups, are left alone.

/** words splits a query at top-level spaces, keeping quoted values and parentheses whole. */
export function words(text: string): string[] {
  const out: string[] = [];
  let cur = "";
  let depth = 0;
  let quoted = false;
  for (const ch of text) {
    if (ch === '"') quoted = !quoted;
    else if (!quoted && ch === "(") depth++;
    else if (!quoted && ch === ")") depth = Math.max(0, depth - 1);
    if (ch === " " && !quoted && depth === 0) {
      if (cur) out.push(cur);
      cur = "";
      continue;
    }
    cur += ch;
  }
  if (cur) out.push(cur);
  return out;
}

/** quote wraps a value in quotes when it holds a space. */
function quote(value: string): string {
  return value.includes(" ") ? `"${value}"` : value;
}

function unquote(value: string): string {
  return value.length >= 2 && value.startsWith('"') && value.endsWith('"') ? value.slice(1, -1) : value;
}

/** termIndex finds a top-level field:value term, ignoring negated ones. */
function termIndex(ws: string[], field: string): number {
  return ws.findIndex((w) => w.toLowerCase().startsWith(field.toLowerCase() + ":"));
}

/** getTerm is the value of the query's top-level field: term, or null. */
export function getTerm(text: string, field: string): string | null {
  const ws = words(text);
  const i = termIndex(ws, field);
  return i < 0 ? null : unquote(ws[i].slice(field.length + 1));
}

/**
 * setTerm sets the query's field: term to value, replacing one already
 * there, or removes it when value is null.
 */
export function setTerm(text: string, field: string, value: string | null): string {
  const ws = words(text);
  const i = termIndex(ws, field);
  const term = value === null ? null : `${field}:${quote(value)}`;
  if (i >= 0) {
    if (term === null) ws.splice(i, 1);
    else ws[i] = term;
  } else if (term !== null) {
    ws.push(term);
  }
  return ws.join(" ");
}
