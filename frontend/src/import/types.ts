import type { AddressEntry, AddressFieldDefinition } from "../api/types";

export type ImportWarning = {
  row: number;
  field?: string;
  message: string;
};

export type ColumnMapping = {
  fields: ColumnMappingField[];
  headerLabels: string[];
  suggestedEmailColumn: number;
};

export type ColumnMappingFieldOrigin = "standard" | "persisted" | "new";

export type ColumnMappingField = AddressFieldDefinition & {
  sourceIndex: number;
  origin: ColumnMappingFieldOrigin;
};

export type ImportResult = {
  entries: AddressEntry[];
  fields?: AddressFieldDefinition[];
  warnings: ImportWarning[];
  columnMapping?: ColumnMapping;
  rows?: string[][];
};
