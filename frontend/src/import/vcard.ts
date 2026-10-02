import {
  MAX_IMPORT_ROWS,
  addImportWarning,
  createAddressListEntry,
  stripBOM,
} from "./shared";
import type { AddressFieldDefinition } from "../api/types";
import type { ImportResult, ImportWarning } from "./types";

type VCardProperty = {
  name: string;
  value: string;
};

export function parseVCardAddressList(
  text: string,
  definitions: AddressFieldDefinition[],
): ImportResult {
  const warnings: ImportWarning[] = [];
  const blocks = vcardBlocks(stripBOM(text), warnings);
  const entries = [];
  contacts: for (const block of blocks) {
    const contact = parseVCardBlock(block);
    if (!contact) continue;
    for (const email of contact.emails) {
      if (entries.length === MAX_IMPORT_ROWS) {
        addImportWarning(warnings, MAX_IMPORT_ROWS + 1, `File truncated after ${MAX_IMPORT_ROWS} address entries.`);
        break contacts;
      }
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
  }
  return { entries, fields: definitions.map((field) => ({ ...field })), warnings };
}

function parseVCardBlock(block: string) {
  const properties = unfoldVCardLines(block)
    .map(parsePropertyLine)
    .filter((item): item is VCardProperty => item !== null);
  const hasContent = properties.some((property) => {
    if (property.name === "VERSION") return false;
    const values = property.name === "N"
      ? splitStructuredValue(property.value)
      : [unescapeVCardText(property.value)];
    return values.some((value) => value.trim() !== "");
  });
  if (!hasContent) return null;
  const structuredName = properties.find((property) => property.name === "N");
  let firstName = "";
  let lastName = "";
  if (structuredName) {
    const parts = splitStructuredValue(structuredName.value);
    lastName = parts[0] ?? "";
    firstName = parts[1] ?? "";
  }
  const emails = properties
    .filter((property) => property.name === "EMAIL")
    .map((property) => unescapeVCardText(property.value).replace(/^mailto:/i, ""));
  return {
    emails: emails.length > 0 ? emails : [""],
    firstName,
    lastName,
  };
}

function vcardBlocks(text: string, warnings: ImportWarning[]): string[] {
  const blocks: string[] = [];
  const pattern = /BEGIN:VCARD([\s\S]*?)END:VCARD/gi;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    blocks.push(match[1] ?? "");
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
  const rawName = line.slice(0, separator).split(";", 1)[0] ?? "";
  return {
    name: (rawName.includes(".") ? rawName.split(".").pop() ?? rawName : rawName).toUpperCase(),
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
