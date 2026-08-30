import {
  MAX_IMPORT_ROWS,
  addImportWarning,
  createAddressListEntry,
  isValidEmail,
  stripBOM,
  trimImportField,
} from "./shared";
import type { AddressFieldDefinition } from "../api/types";
import type { ImportResult, ImportWarning } from "./types";

type VCardProperty = {
  name: string;
  params: string[];
  value: string;
};

export function parseVCardAddressList(
  text: string,
  definitions: AddressFieldDefinition[],
): ImportResult {
  const warnings: ImportWarning[] = [];
  const blocks = vcardBlocks(stripBOM(text), warnings);
  const entries = [];
  const seenEmails = new Set<string>();
  for (const [index, block] of blocks.slice(0, MAX_IMPORT_ROWS).entries()) {
    const rowNumber = index + 1;
    const contact = parseVCardBlock(block, rowNumber, warnings);
    if (!contact.email) {
      addImportWarning(warnings, rowNumber, `vCard ${rowNumber}: skipped - no valid email found.`, "email");
      continue;
    }
    const email = contact.email.toLowerCase();
    if (seenEmails.has(email)) {
      addImportWarning(warnings, rowNumber, `vCard ${rowNumber}: duplicate email "${email}" skipped.`, "email");
      continue;
    }
    seenEmails.add(email);
    const values = Object.fromEntries(definitions.map((field) => {
      switch (field.role) {
      case "email":
        return [field.key, email];
      case "first_name":
        return [field.key, contact.firstName];
      case "last_name":
        return [field.key, contact.lastName];
      default:
        return [field.key, ""];
      }
    }));
    entries.push(createAddressListEntry(email, definitions, values));
  }
  if (blocks.length > MAX_IMPORT_ROWS) {
    addImportWarning(warnings, MAX_IMPORT_ROWS + 1, `File truncated after ${MAX_IMPORT_ROWS} vCards.`);
  }
  return { entries, fields: definitions.map((field) => ({ ...field })), warnings };
}

function parseVCardBlock(block: string, rowNumber: number, warnings: ImportWarning[]) {
  const properties = unfoldVCardLines(block)
    .map(parsePropertyLine)
    .filter((item): item is VCardProperty => item !== null);
  const structuredName = properties.find((property) => property.name === "N");
  let firstName = "";
  let lastName = "";
  if (structuredName) {
    const parts = splitStructuredValue(structuredName.value);
    lastName = parts[0] ?? "";
    firstName = parts[1] ?? "";
  }
  const validEmails = properties
    .filter((property) => property.name === "EMAIL")
    .map((property) => unescapeVCardText(property.value).replace(/^mailto:/i, "").trim())
    .filter((email) => {
      if (!email) return false;
      if (!isValidEmail(email)) {
        addImportWarning(warnings, rowNumber, `vCard ${rowNumber}: ignored invalid email "${email}".`, "email");
        return false;
      }
      return true;
  });
  for (const extraEmail of validEmails.slice(1)) {
    addImportWarning(
      warnings,
      rowNumber,
      `vCard ${rowNumber}: additional email "${extraEmail}" dropped (only one email per entry).`,
      "email",
    );
  }
  return {
    email: trimImportField(validEmails[0] ?? "", rowNumber, "email", warnings),
    firstName: trimImportField(firstName, rowNumber, "first name", warnings),
    lastName: trimImportField(lastName, rowNumber, "last name", warnings),
  };
}

function vcardBlocks(text: string, warnings: ImportWarning[]): string[] {
  const blocks: string[] = [];
  const pattern = /BEGIN:VCARD([\s\S]*?)END:VCARD/gi;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    blocks.push(match[1] ?? "");
    if (blocks.length > MAX_IMPORT_ROWS) break;
  }
  if (blocks.length === 0 && text.trim()) addImportWarning(warnings, 1, "No vCard entries were found.");
  return blocks;
}

function unfoldVCardLines(block: string): string[] {
  return block.split(/\r\n|\n|\r/).reduce<string[]>((unfolded, line) => {
    if (/^[ \t]/.test(line) && unfolded.length > 0) unfolded[unfolded.length - 1] += line.slice(1);
    else if (line.trim()) unfolded.push(line);
    return unfolded;
  }, []);
}

function parsePropertyLine(line: string): VCardProperty | null {
  const separator = line.indexOf(":");
  if (separator < 0) return null;
  const nameParts = line.slice(0, separator).split(";");
  const rawName = nameParts.shift() ?? "";
  return {
    name: (rawName.includes(".") ? rawName.split(".").pop() ?? rawName : rawName).toUpperCase(),
    params: nameParts.map((value) => value.toUpperCase()),
    value: line.slice(separator + 1),
  };
}

function splitStructuredValue(value: string): string[] {
  const parts: string[] = [];
  let field = "";
  let escaped = false;
  for (const char of value) {
    if (escaped) {
      field += unescapeVCardCharacter(char);
      escaped = false;
    } else if (char === "\\") escaped = true;
    else if (char === ";") {
      parts.push(unescapeVCardText(field));
      field = "";
    } else field += char;
  }
  parts.push(unescapeVCardText(field));
  return parts;
}

function unescapeVCardCharacter(value: string): string {
  return value === "n" || value === "N" ? "\n" : value;
}

function unescapeVCardText(value: string): string {
  return value.replace(/\\([nN;,\\])/g, (_, escaped: string) => unescapeVCardCharacter(escaped));
}
