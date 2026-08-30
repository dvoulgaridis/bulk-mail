import {
  MAX_IMPORT_ROWS,
  addTruncationWarning,
  detectColumnMappingDetails,
  stripBOM,
} from "./shared";
import type { AddressFieldDefinition } from "../api/types";
import type { ImportResult, ImportWarning } from "./types";

export function parseDelimitedAddressList(
  text: string,
  delimiter: string,
  definitions: AddressFieldDefinition[],
): ImportResult {
  const parsed = splitDelimitedRows(stripBOM(text), delimiter, MAX_IMPORT_ROWS + 1);
  if (parsed.rows.length === 0) return { entries: [], warnings: [] };
  const warnings: ImportWarning[] = [];
  addTruncationWarning(warnings, parsed.truncated);
  return {
    entries: [],
    warnings,
    columnMapping: detectColumnMappingDetails(
      parsed.rows[0] ?? [],
      definitions,
      parsed.rows.slice(1),
    ),
    rows: parsed.rows,
  };
}

function splitDelimitedRows(
  text: string,
  delimiter: string,
  maxRows: number,
): { rows: string[][]; truncated: boolean } {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (quoted) {
      if (char === '"' && next === '"') {
        field += '"';
        index += 1;
      } else if (char === '"') quoted = false;
      else field += char;
      continue;
    }
    if (char === '"') quoted = true;
    else if (char === delimiter) {
      row.push(field);
      field = "";
    } else if (char === "\n") {
      row.push(field);
      if (pushDelimitedRow(rows, row, maxRows)) return { rows, truncated: true };
      row = [];
      field = "";
    } else if (char !== "\r") field += char;
  }
  row.push(field);
  return { rows, truncated: pushDelimitedRow(rows, row, maxRows) };
}

function pushDelimitedRow(rows: string[][], row: string[], maxRows: number): boolean {
  if (!row.some((value) => value.trim() !== "")) return false;
  if (rows.length >= maxRows) return true;
  rows.push(row);
  return false;
}
