import type { AddressEntry, AddressFieldDefinition } from "../api/types";
import { addressFieldValue } from "./shared";

type ExportableList = {
  fields: AddressFieldDefinition[];
  entries: AddressEntry[];
};

export function exportAddressListAsCSV(list: ExportableList): string {
  const rows = [
    list.fields.map((field) => field.label),
    ...list.entries.map((entry) => list.fields.map((field) => (
      field.role === "email"
        ? entry.email
        : addressFieldValue(entry.fields, field.key)
    ))),
  ];
  return rows.map((row) => row.map(csvEscape).join(",")).join("\r\n");
}

export function exportAddressListAsVCard(list: ExportableList): string {
  const firstNameKey = list.fields.find((field) => field.role === "first_name")?.key;
  const lastNameKey = list.fields.find((field) => field.role === "last_name")?.key;
  const content = list.entries
    .flatMap((entry) => {
      const firstName = firstNameKey ? addressFieldValue(entry.fields, firstNameKey) : "";
      const lastName = lastNameKey ? addressFieldValue(entry.fields, lastNameKey) : "";
      const displayName = [firstName, lastName].filter(Boolean).join(" ") || entry.email;
      return [
        "BEGIN:VCARD",
        "VERSION:3.0",
        `N:${escapeVCardText(lastName)};${escapeVCardText(firstName)};;;`,
        `FN:${escapeVCardText(displayName)}`,
        `EMAIL;TYPE=INTERNET:${entry.email}`,
        "END:VCARD",
      ];
    })
    .join("\r\n");
  return content ? `${content}\r\n` : "";
}

function csvEscape(value: string): string {
  return /[",\n\r]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}

function escapeVCardText(value: string): string {
  return value
    .replaceAll("\\", "\\\\")
    .replaceAll(";", "\\;")
    .replaceAll(",", "\\,")
    .replaceAll("\r\n", "\\n")
    .replaceAll("\n", "\\n")
    .replaceAll("\r", "\\n");
}
