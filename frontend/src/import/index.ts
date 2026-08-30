import { parseDelimitedAddressList } from "./delimited";
import { parseXLSXAddressList } from "./spreadsheet";
import type { AddressFieldDefinition } from "../api/types";
import type { ImportResult } from "./types";
import { parseVCardAddressList } from "./vcard";

const MAX_FILE_SIZE_BYTES = 10 * 1024 * 1024;

export async function parseAddressListFile(
  file: File,
  definitions: AddressFieldDefinition[],
): Promise<ImportResult> {
  if (file.size > MAX_FILE_SIZE_BYTES) throw new Error("Address list imports are limited to 10 MB.");
  const name = file.name.toLowerCase();
  if (name.endsWith(".xlsx")) return parseXLSXAddressList(await file.arrayBuffer(), definitions);
  if (name.endsWith(".vcf") || name.endsWith(".vcard")) {
    return parseVCardAddressList(await file.text(), definitions);
  }
  return parseDelimitedAddressList(
    await file.text(),
    name.endsWith(".tsv") ? "\t" : ",",
    definitions,
  );
}

export {
  MAX_ADDRESS_FIELD_CHARACTERS,
  MAX_IMPORT_WARNINGS,
  MAX_PLACEHOLDER_KEY_CHARACTERS,
  addressEntryDisplayName,
  addressFieldValue,
  applyColumnMappingToRows,
  createAddressListEntry,
  suggestPlaceholderKey,
  validateColumnMapping,
} from "./shared";
export type {
  ColumnMapping,
  ColumnMappingField,
  ImportResult,
  ImportWarning,
} from "./types";
