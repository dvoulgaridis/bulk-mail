import type { AddressEntry, AddressFieldDefinition, AddressFieldRole } from "../api/types";
import type { ColumnMapping, ColumnMappingField, ImportResult, ImportWarning } from "./types";

export const MAX_IMPORT_ROWS = 10000;
export const MAX_IMPORT_WARNINGS = 50;
export const MAX_ADDRESS_FIELD_CHARACTERS = 255;
export const MAX_PLACEHOLDER_KEY_CHARACTERS = 64;

const emailHeaders = new Set(["email", "e_mail", "mail", "email_address", "e_mail_value"]);
const firstNameHeaders = new Set(["first_name", "firstname", "first", "given_name", "given"]);
const lastNameHeaders = new Set(["last_name", "lastname", "last", "surname", "family_name", "family"]);

export function detectColumnMappingDetails(
  headers: string[],
  definitions: AddressFieldDefinition[],
  sampleRows: string[][] = [],
): ColumnMapping {
  const normalized = headers.map(normalizeDetectionHeader);
  const emailMatches = matchingHeaders(normalized, emailHeaders);
  const emailSource = emailMatches.length === 1 ? emailMatches[0] : -1;
  const suggestedEmailColumn = emailSource < 0 && emailMatches.length === 0
    ? suggestedEmailSource(sampleRows, headers.length)
    : -1;
  const fields = definitions.map((definition) => mappingField(
    detectedFieldSource(definition.role, normalized, emailSource),
    definition,
  ));
  return {
    fields,
    headerLabels: headers.map((header, index) => header.trim() || `Column ${index + 1}`),
    suggestedEmailColumn,
  };
}

export function applyColumnMappingToRows(rows: string[][], mapping: ColumnMapping, truncated = false): ImportResult {
  const warnings: ImportWarning[] = [];
  const mappedFields = mapping.fields.map((field, position) => ({
    definition: {
      key: normalizePlaceholderKey(field.key),
      label: field.label.trim().normalize("NFC"),
      role: field.role,
      position,
    },
    sourceIndex: field.sourceIndex,
  }));
  const definitions = mappedFields.map((field) => field.definition);
  const emailMapping = mappedFields.find((field) => field.definition.role === "email");
  if (!emailMapping || emailMapping.sourceIndex < 0) throw new Error("Choose the address email column.");
  const seenEmails = new Set<string>();
  const entries = rows.slice(1, MAX_IMPORT_ROWS + 1).reduce<AddressEntry[]>((items, row, index) => {
    const rowNumber = index + 2;
    const fields = Object.create(null) as Record<string, string>;
    for (const field of mappedFields) {
      fields[field.definition.key] = importCell(
        row,
        field.sourceIndex,
        rowNumber,
        field.definition.label,
        warnings,
      );
    }
    const email = (fields[emailMapping.definition.key] || "").toLowerCase();
    if (!isValidEmail(email)) {
      addImportWarning(warnings, rowNumber, `Row ${rowNumber}: skipped - no valid email found.`, "email");
      return items;
    }
    if (seenEmails.has(email)) {
      addImportWarning(warnings, rowNumber, `Row ${rowNumber}: duplicate email "${email}" skipped.`, "email");
      return items;
    }
    seenEmails.add(email);
    items.push(createAddressListEntry(email, definitions, fields));
    return items;
  }, []);
  addTruncationWarning(warnings, truncated || rows.length > MAX_IMPORT_ROWS + 1);
  return { entries, fields: definitions, warnings };
}

export function createAddressListEntry(
  email: string,
  definitions: AddressFieldDefinition[],
  values: Record<string, string> = {},
): AddressEntry {
  const fields = Object.fromEntries(definitions.map((field) => [field.key, addressFieldValue(values, field.key)]));
  const emailField = definitions.find((field) => field.role === "email");
  if (emailField) fields[emailField.key] = email;
  return {
    id: 0,
    email,
    displayName: addressEntryDisplayName(fields, email, definitions),
    fields,
  };
}

export function addressEntryDisplayName(
  fields: Record<string, string>,
  email: string,
  definitions: AddressFieldDefinition[],
): string {
  const combined = definitions
    .filter((field) => field.role === "first_name" || field.role === "last_name")
    .map((field) => titleCaseAddressField(addressFieldValue(fields, field.key)))
    .filter(Boolean)
    .join(" ");
  return combined || email.trim().toLowerCase();
}

export function addressFieldValue(fields: Record<string, string>, key: string): string {
  return Object.prototype.hasOwnProperty.call(fields, key) ? fields[key] ?? "" : "";
}

export function addImportWarning(warnings: ImportWarning[], row: number, message: string, field?: string): void {
  if (warnings.length >= MAX_IMPORT_WARNINGS) return;
  warnings.push(field ? { row, field, message } : { row, message });
}

export function addTruncationWarning(warnings: ImportWarning[], truncated: boolean): void {
  if (truncated) addImportWarning(warnings, MAX_IMPORT_ROWS + 1, `File truncated after ${MAX_IMPORT_ROWS} rows.`);
}

export function trimImportField(value: string, row: number, field: string, warnings: ImportWarning[]): string {
  const trimmed = value.trim();
  const characters = Array.from(trimmed);
  if (characters.length <= MAX_ADDRESS_FIELD_CHARACTERS) return trimmed;
  addImportWarning(
    warnings,
    row,
    `Row ${row}: ${field} truncated to ${MAX_ADDRESS_FIELD_CHARACTERS} characters.`,
    field,
  );
  return characters.slice(0, MAX_ADDRESS_FIELD_CHARACTERS).join("");
}

function normalizePlaceholderKey(value: string): string {
  const key = value.trim().normalize("NFC").toLowerCase();
  if (!key) throw new Error("Placeholder key is required.");
  if (Array.from(key).length > MAX_PLACEHOLDER_KEY_CHARACTERS) {
    throw new Error(`Placeholder keys are limited to ${MAX_PLACEHOLDER_KEY_CHARACTERS} characters.`);
  }
  if (!/^[\p{L}\p{N}\p{M}_.-]+$/u.test(key)) {
    throw new Error(`Placeholder key "${value}" contains unsupported characters.`);
  }
  return key;
}

export function suggestPlaceholderKey(value: string, existingKeys: Iterable<string>): string {
  const unavailable = new Set(["full_name"]);
  for (const key of existingKeys) {
    if (!key.trim()) continue;
    try {
      unavailable.add(normalizePlaceholderKey(key));
    } catch {
      continue;
    }
  }
  let base = value
    .trim()
    .normalize("NFC")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\p{M}_.-]+/gu, "_")
    .replace(/^[_.-]+|[_.-]+$/gu, "");
  if (!base) base = "field";
  base = Array.from(base).slice(0, MAX_PLACEHOLDER_KEY_CHARACTERS).join("");
  let candidate = base;
  for (let suffixNumber = 2; unavailable.has(candidate); suffixNumber += 1) {
    const suffix = `_${suffixNumber}`;
    candidate = Array.from(base)
      .slice(0, MAX_PLACEHOLDER_KEY_CHARACTERS - suffix.length)
      .join("") + suffix;
  }
  return candidate;
}

export function validateColumnMapping(fields: ColumnMappingField[], maxFields: number): string {
  if (maxFields < 1 || fields.length > maxFields) {
    return `Address lists support at most ${maxFields} fields.`;
  }
  const emailField = fields.find((field) => field.role === "email");
  if (!emailField || emailField.sourceIndex < 0) return "Choose the address email column.";
  const sources = new Set<number>();
  const keys = new Set<string>();
  for (const field of fields) {
    if (field.origin === "new" && field.sourceIndex < 0) {
      return `Choose a source column for ${field.label.trim() || "the new custom field"}.`;
    }
    if (field.sourceIndex >= 0) {
      if (sources.has(field.sourceIndex)) return "Each source column can be mapped only once.";
      sources.add(field.sourceIndex);
    }
    if (!field.label.trim()) return "Custom field labels are required.";
    if (Array.from(field.label.trim()).length > MAX_ADDRESS_FIELD_CHARACTERS) {
      return `Custom field labels are limited to ${MAX_ADDRESS_FIELD_CHARACTERS} characters.`;
    }
    let key: string;
    try {
      key = normalizePlaceholderKey(field.key);
    } catch (error) {
      return error instanceof Error ? error.message : String(error);
    }
    if (key === "full_name") return "full_name is reserved for the derived full name placeholder.";
    if (keys.has(key)) return `Placeholder key "${key}" is already mapped.`;
    keys.add(key);
  }
  return "";
}

export function isValidEmail(value: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value);
}

export function stripBOM(text: string): string {
  return text.startsWith("\uFEFF") ? text.slice(1) : text;
}

function mappingField(sourceIndex: number, definition: AddressFieldDefinition): ColumnMappingField {
  return {
    sourceIndex,
    origin: definition.role === "" ? "persisted" : "standard",
    ...definition,
  };
}

function detectedFieldSource(role: AddressFieldRole, headers: string[], emailSource: number): number {
  switch (role) {
  case "email":
    return emailSource;
  case "first_name":
    return detectedSource(headers, firstNameHeaders);
  case "last_name":
    return detectedSource(headers, lastNameHeaders);
  default:
    return -1;
  }
}

function detectedSource(headers: string[], candidates: Set<string>): number {
  const matches = matchingHeaders(headers, candidates);
  return matches.length === 1 ? matches[0] : -1;
}

function titleCaseAddressField(value: string): string {
  return value
    .trim()
    .replace(/\s+/gu, " ")
    .normalize("NFC")
    .toLocaleLowerCase()
    .split(" ")
    .map((part) => {
      const characters = Array.from(part);
      return characters.length === 0
        ? ""
        : characters[0].toLocaleUpperCase() + characters.slice(1).join("");
    })
    .join(" ");
}

function matchingHeaders(headers: string[], candidates: Set<string>): number[] {
  return headers.reduce<number[]>((matches, header, index) => {
    if (candidates.has(header)) matches.push(index);
    return matches;
  }, []);
}

function suggestedEmailSource(rows: string[][], columnCount: number): number {
  const candidates: number[] = [];
  for (let column = 0; column < columnCount; column += 1) {
    const values = rows.slice(0, 20).map((row) => (row[column] || "").trim()).filter(Boolean);
    if (values.length > 0 && values.every(isValidEmail)) candidates.push(column);
  }
  return candidates.length === 1 ? candidates[0] : -1;
}

function importCell(row: string[], index: number, rowNumber: number, field: string, warnings: ImportWarning[]): string {
  return index >= 0 ? trimImportField(row[index] ?? "", rowNumber, field, warnings) : "";
}

function normalizeDetectionHeader(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}
