import {
  MAX_IMPORT_ROWS,
  addImportWarning,
  detectColumnMappingDetails,
} from "./shared";
import type { AddressFieldDefinition } from "../api/types";
import type { ImportResult, ImportWarning } from "./types";

type ZipEntry = {
  name: string;
  method: number;
  compressedSize: number;
  localOffset: number;
};

export async function parseXLSXAddressList(
  buffer: ArrayBuffer,
  definitions: AddressFieldDefinition[],
): Promise<ImportResult> {
  const entries = readZipEntries(buffer);
  const sharedStrings = parseSharedStrings(await zipText(entries, buffer, "xl/sharedStrings.xml"));
  const worksheetName = findWorksheetName(entries);
  if (!worksheetName) throw new Error("XLSX file does not contain a worksheet.");
  const rows = parseWorksheetRows(await zipText(entries, buffer, worksheetName), sharedStrings);
  if (rows.length === 0) return { entries: [], warnings: [] };
  const warnings: ImportWarning[] = [];
  if (rows.length > MAX_IMPORT_ROWS + 1) {
    addImportWarning(warnings, MAX_IMPORT_ROWS + 1, `File truncated after ${MAX_IMPORT_ROWS} rows.`);
  }
  const limitedRows = rows.slice(0, MAX_IMPORT_ROWS + 1);
  return {
    entries: [],
    warnings,
    columnMapping: detectColumnMappingDetails(
      limitedRows[0] ?? [],
      definitions,
      limitedRows.slice(1),
    ),
    rows: limitedRows,
  };
}

function readZipEntries(buffer: ArrayBuffer): Map<string, ZipEntry> {
  const view = new DataView(buffer);
  const decoder = new TextDecoder();
  let endOffset = -1;
  for (let index = view.byteLength - 22; index >= 0; index -= 1) {
    if (view.getUint32(index, true) === 0x06054b50) {
      endOffset = index;
      break;
    }
  }
  if (endOffset < 0) throw new Error("XLSX file is not a valid ZIP archive.");
  const entryCount = view.getUint16(endOffset + 10, true);
  let cursor = view.getUint32(endOffset + 16, true);
  const entries = new Map<string, ZipEntry>();
  for (let index = 0; index < entryCount; index += 1) {
    if (view.getUint32(cursor, true) !== 0x02014b50) throw new Error("XLSX central directory is invalid.");
    const method = view.getUint16(cursor + 10, true);
    const compressedSize = view.getUint32(cursor + 20, true);
    const nameLength = view.getUint16(cursor + 28, true);
    const extraLength = view.getUint16(cursor + 30, true);
    const commentLength = view.getUint16(cursor + 32, true);
    const localOffset = view.getUint32(cursor + 42, true);
    const name = decoder.decode(new Uint8Array(buffer, cursor + 46, nameLength));
    entries.set(name, { name, method, compressedSize, localOffset });
    cursor += 46 + nameLength + extraLength + commentLength;
  }
  return entries;
}

async function zipText(entries: Map<string, ZipEntry>, buffer: ArrayBuffer, name: string): Promise<string> {
  const entry = entries.get(name);
  if (!entry) return "";
  return new TextDecoder().decode(await zipEntryBytes(buffer, entry));
}

async function zipEntryBytes(buffer: ArrayBuffer, entry: ZipEntry): Promise<Uint8Array> {
  const view = new DataView(buffer);
  let cursor = entry.localOffset;
  if (view.getUint32(cursor, true) !== 0x04034b50) throw new Error("XLSX local file header is invalid.");
  const nameLength = view.getUint16(cursor + 26, true);
  const extraLength = view.getUint16(cursor + 28, true);
  cursor += 30 + nameLength + extraLength;
  const compressed = new Uint8Array(buffer, cursor, entry.compressedSize);
  if (entry.method === 0) return compressed;
  if (entry.method !== 8) throw new Error("XLSX compression method is not supported.");
  if (!globalThis.DecompressionStream) throw new Error("XLSX import requires browser ZIP decompression support.");
  const stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream("deflate-raw"));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

function findWorksheetName(entries: Map<string, ZipEntry>): string | null {
  if (entries.has("xl/worksheets/sheet1.xml")) return "xl/worksheets/sheet1.xml";
  return Array.from(entries.keys()).filter((name) => /^xl\/worksheets\/sheet\d+\.xml$/.test(name)).sort()[0] ?? null;
}

function parseSharedStrings(xml: string): string[] {
  if (!xml.trim()) return [];
  const document = new DOMParser().parseFromString(xml, "application/xml");
  return Array.from(document.getElementsByTagName("si")).map((item) =>
    Array.from(item.getElementsByTagName("t")).map((node) => node.textContent ?? "").join(""),
  );
}

function parseWorksheetRows(xml: string, sharedStrings: string[]): string[][] {
  const document = new DOMParser().parseFromString(xml, "application/xml");
  return Array.from(document.getElementsByTagName("row")).map((row) => {
    const values: string[] = [];
    Array.from(row.getElementsByTagName("c")).forEach((cell) => {
      values[columnRefToIndex(cell.getAttribute("r") ?? "")] = worksheetCellValue(cell, sharedStrings);
    });
    return values;
  });
}

function worksheetCellValue(cell: Element, sharedStrings: string[]): string {
  const type = cell.getAttribute("t");
  if (type === "s") return sharedStrings[Number(cell.getElementsByTagName("v")[0]?.textContent ?? "")] ?? "";
  if (type === "inlineStr") {
    return Array.from(cell.getElementsByTagName("t")).map((node) => node.textContent ?? "").join("");
  }
  return (cell.getElementsByTagName("v")[0]?.textContent ?? "").trim();
}

function columnRefToIndex(ref: string): number {
  const letters = ref.replace(/[0-9]/g, "").toUpperCase();
  let index = 0;
  for (const letter of letters) index = index * 26 + letter.charCodeAt(0) - 64;
  return Math.max(0, index - 1);
}
