import {
  computed,
  inject,
  provide,
  reactive,
  ref,
  useTemplateRef,
  type InjectionKey,
} from "vue";
import type {
  AddressEntry,
  AddressFieldDefinition,
  AddressList,
} from "../../api/types";
import type { WorkspaceContext } from "../../app/context";
import { saveTextFile } from "../../common/files";
import { shortDate } from "../../common/format";
import { exportAddressListAsCSV, exportAddressListAsVCard } from "../../import/export";
import {
  MAX_IMPORT_WARNINGS,
  addressEntryDisplayName,
  addressFieldValue,
  applyColumnMappingToRows,
  createAddressListEntry,
  parseAddressListFile,
  suggestPlaceholderKey,
  validateColumnMapping,
  type ColumnMapping,
  type ColumnMappingField,
  type ImportResult,
  type ImportWarning,
} from "../../import";

type EditableAddressList = Omit<AddressList, "entries"> & {
  entries: AddressEntry[];
};

type PendingImport = {
  fileName: string;
  rows: string[][];
  warnings: ImportWarning[];
  columnMapping: ColumnMapping;
};

type ImportState = {
  fileName: string;
  importedCount: number;
  skippedDuplicateCount: number;
  warnings: ImportWarning[];
  pending: PendingImport | null;
  mappingForm: ColumnMapping;
};

export type AddressListsFeature = ReturnType<typeof createAddressListsFeature>;

const addressListsKey: InjectionKey<AddressListsFeature> = Symbol("addressLists");

export function provideAddressListsFeature(workspace: WorkspaceContext): AddressListsFeature {
  const feature = createAddressListsFeature(workspace);
  provide(addressListsKey, feature);
  return feature;
}

export function useAddressListsFeature(): AddressListsFeature {
  const feature = inject(addressListsKey);
  if (!feature) throw new Error("Address lists feature is unavailable.");
  return feature;
}

function createAddressListsFeature(workspace: WorkspaceContext) {
  const importInput = useTemplateRef<HTMLInputElement>("importInput");
  const listSearch = ref("");
  const entrySearch = ref("");
  const selectedEntryKeys = ref<string[]>([]);
  const selectedList = reactive<EditableAddressList>(emptyAddressList(workspace.state.addressFieldDefaults));
  const importState = reactive<ImportState>({
    fileName: "",
    importedCount: 0,
    skippedDuplicateCount: 0,
    warnings: [],
    pending: null,
    mappingForm: emptyColumnMapping(),
  });

  const listRows = computed(() => {
    const query = listSearch.value.trim().toLowerCase();
    return workspace.state.addressLists
      .filter((list) => !query || [list.name, list.notes || ""].some((value) => value.toLowerCase().includes(query)))
      .map((list) => ({
        id: list.id,
        time: shortDate(list.createdAt),
        name: list.name,
        addresses: String(list.count || 0),
        notes: list.notes || "No notes",
      }));
  });

  const entryRows = computed(() => {
    const query = entrySearch.value.trim().toLowerCase();
    return selectedList.entries
      .map((entry, index) => ({ entry, index }))
      .filter(({ entry }) => !query || [entry.email, ...Object.values(entry.fields)].some((value) =>
        value.toLowerCase().includes(query),
      ));
  });

  const importSummary = computed(() => {
    if (!importState.fileName) return "";
    const parts = [`Imported ${importState.importedCount} addresses from ${importState.fileName}.`];
    if (importState.skippedDuplicateCount > 0) {
      parts.push(`${importState.skippedDuplicateCount} duplicate addresses skipped.`);
    }
    return parts.join(" ");
  });

  const omittedColumnCount = computed(() => {
    const pending = importState.pending;
    if (!pending) return 0;
    const mapped = new Set(
      importState.mappingForm.fields
        .map((field) => field.sourceIndex)
        .filter((index) => index >= 0),
    );
    return pending.columnMapping.headerLabels.length - mapped.size;
  });

  const canAddCustomMapping = computed(() => {
    const limit = workspace.state.limits.maxAddressListFields;
    return limit > 0 && importState.mappingForm.fields.length < limit;
  });

  const entryGridStyle = computed(() => ({
    gridTemplateColumns: `minmax(3rem, 4rem) repeat(${Math.max(selectedList.fields.length, 1)}, minmax(11rem, 1fr))`,
  }));

  function openNewAddressList(): void {
    Object.assign(selectedList, emptyAddressList(workspace.state.addressFieldDefaults));
    clearImportState();
    selectedEntryKeys.value = [];
    workspace.navigate("address-list-detail");
  }

  async function edit(id: number): Promise<void> {
    await workspace.runAction(async () => {
      await requestAddressList(id);
      workspace.navigate("address-list-detail");
    });
  }

  async function requestAddressList(id: number): Promise<void> {
    const addressList = await workspace.api.request<AddressList>(`/api/address-lists/${id}`);
    selectAddressList(addressList);
  }

  function selectAddressList(addressList: AddressList): void {
    const fields = addressList.fields.map((field) => ({ ...field }));
    Object.assign(selectedList, {
      ...addressList,
      notes: addressList.notes || "",
      fields,
      entries: (addressList.entries || []).map((entry) => normalizeAddressEntry(entry, fields)),
    });
    ensureEntryFields();
    clearImportState();
    selectedEntryKeys.value = [];
  }

  function addEntry(): void {
    selectedList.entries.push(createAddressListEntry("", selectedList.fields));
  }

  function entryKey(entry: AddressEntry, index: number): string {
    return entry.id > 0 ? String(entry.id) : `new-${index}`;
  }

  function deleteSelectedEntries(): void {
    if (selectedEntryKeys.value.length === 0) return;
    const selected = new Set(selectedEntryKeys.value);
    selectedList.entries = selectedList.entries.filter((entry, index) => !selected.has(entryKey(entry, index)));
    selectedEntryKeys.value = [];
  }

  async function suppressSelectedEntries(): Promise<void> {
    if (selectedEntryKeys.value.length === 0) return;
    const selected = new Set(selectedEntryKeys.value);
    const emails = selectedList.entries
      .filter((entry, index) => selected.has(entryKey(entry, index)))
      .map((entry) => entry.email.trim())
      .filter(Boolean);
    if (emails.length === 0) {
      workspace.notify("Selected rows do not contain email addresses.", "error");
      return;
    }
    await workspace.runAction(async () => {
      await workspace.api.request("/api/suppressions", { method: "POST", body: { emails, reason: "address list" } });
      await workspace.refresh();
      workspace.notify(`${emails.length} selected address${emails.length === 1 ? "" : "es"} added to suppressions.`);
    });
  }

  function selectAllEntries(): void {
    selectedEntryKeys.value = selectedList.entries.map(entryKey);
  }

  function openImportPicker(): void {
    importInput.value?.click();
  }

  async function handleImportChange(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importState.fileName = file.name;
    importState.importedCount = 0;
    importState.skippedDuplicateCount = 0;
    importState.warnings = [];
    try {
      const result = await parseAddressListFile(file, selectedList.fields);
      if (result.columnMapping && result.rows) {
        importState.pending = {
          fileName: file.name,
          rows: result.rows,
          warnings: result.warnings,
          columnMapping: result.columnMapping,
        };
        importState.mappingForm = cloneColumnMapping(result.columnMapping);
        workspace.navigate("mapping");
      } else applyImportResult(result);
    } catch (error) {
      workspace.notify(error instanceof Error ? error.message : String(error), "error");
    } finally {
      input.value = "";
    }
  }

  function mappingPreview(field: ColumnMappingField): string {
    const pending = importState.pending;
    if (!pending || field.sourceIndex < 0) return "No source column selected";
    const values = pending.rows.slice(1, 4).map((row) => (row[field.sourceIndex] || "").trim()).filter(Boolean);
    return values.length > 0 ? values.join(" · ") : "No sample values";
  }

  function addCustomMapping(): void {
    const pending = importState.pending;
    if (!pending) return;
    if (!canAddCustomMapping.value) {
      workspace.notify(
        `Address lists support at most ${workspace.state.limits.maxAddressListFields} fields.`,
        "error",
      );
      return;
    }
    const mappedSources = new Set(importState.mappingForm.fields.map((field) => field.sourceIndex));
    const sourceIndex = pending.columnMapping.headerLabels.findIndex((_, index) => !mappedSources.has(index));
    const label = sourceIndex >= 0 ? pending.columnMapping.headerLabels[sourceIndex] : "";
    importState.mappingForm.fields.push({
      key: label ? suggestPlaceholderKey(label, importState.mappingForm.fields.map((field) => field.key)) : "",
      label,
      role: "",
      position: importState.mappingForm.fields.length,
      sourceIndex,
      origin: "new",
    });
  }

  function removeCustomMapping(index: number): void {
    if (importState.mappingForm.fields[index]?.origin !== "new") return;
    importState.mappingForm.fields.splice(index, 1);
  }

  function updateMappingSource(field: ColumnMappingField): void {
    const pending = importState.pending;
    if (!pending || field.origin !== "new" || field.sourceIndex < 0) return;
    const label = pending.columnMapping.headerLabels[field.sourceIndex] || "";
    if (!field.label.trim()) field.label = label;
    if (!field.key.trim()) {
      field.key = suggestPlaceholderKey(
        label,
        importState.mappingForm.fields
          .filter((item) => item !== field)
          .map((item) => item.key),
      );
    }
  }

  function cancelMapping(): void {
    clearImportState();
    workspace.navigate("address-list-detail");
  }

  function applyMapping(): void {
    const pending = importState.pending;
    if (!pending) {
      workspace.navigate("address-list-detail");
      return;
    }
    const error = validateColumnMapping(
      importState.mappingForm.fields,
      workspace.state.limits.maxAddressListFields,
    );
    if (error) {
      workspace.notify(error, "error");
      return;
    }
    try {
      const result = applyColumnMappingToRows(pending.rows, cloneColumnMapping(importState.mappingForm));
      result.warnings = [...pending.warnings, ...result.warnings].slice(0, MAX_IMPORT_WARNINGS);
      applyImportResult(result);
      importState.pending = null;
      workspace.navigate("address-list-detail");
    } catch (error) {
      workspace.notify(error instanceof Error ? error.message : String(error), "error");
    }
  }

  async function save(): Promise<void> {
    const existing = selectedList.id > 0;
    await workspace.runAction(async () => {
      const path = selectedList.id
        ? `/api/address-lists/${selectedList.id}`
        : "/api/address-lists/import";
      const list = await workspace.api.request<AddressList>(path, {
        method: selectedList.id ? "PUT" : "POST",
        body: {
          name: selectedList.name.trim(),
          source: selectedList.source,
          notes: selectedList.notes.trim(),
          fields: selectedList.fields.map((field) => ({ ...field })),
          entries: selectedList.entries.map((entry) => addressEntryWritePayload(entry, selectedList.fields)),
        },
      });
      await workspace.refresh();
      selectAddressList(list);
      workspace.notify(existing ? "Address list saved." : "Address list created.");
    });
  }

  async function remove(): Promise<void> {
    if (!selectedList.id || !window.confirm("Delete this address list?")) return;
    await workspace.runAction(async () => {
      await workspace.api.request<void>(`/api/address-lists/${selectedList.id}`, { method: "DELETE" });
      await workspace.refresh();
      workspace.navigate("address-lists");
      workspace.notify("Address list deleted.");
    });
  }

  function exportList(): void {
    const name = selectedList.name || "address-list";
    const format = window.confirm("Export as vCard? Choose Cancel for CSV.") ? "vcard" : "csv";
    const content = format === "csv" ? exportAddressListAsCSV(selectedList) : exportAddressListAsVCard(selectedList);
    saveTextFile(
      `${name}.${format === "csv" ? "csv" : "vcf"}`,
      content,
      format === "csv" ? "text/csv;charset=utf-8" : "text/vcard;charset=utf-8",
    );
  }

  function applyImportResult(result: ImportResult): void {
    if (!result.fields) throw new Error("The imported address fields are unavailable.");
    selectedList.fields = result.fields.map((field) => ({ ...field }));
    const appended = appendImportedEntries(result.entries, [...result.warnings]);
    selectedList.entries = [...selectedList.entries, ...appended.entries];
    ensureEntryFields();
    refreshEntryDisplayNames();
    importState.importedCount = appended.entries.length;
    importState.skippedDuplicateCount = appended.skippedDuplicates;
    importState.warnings = appended.warnings;
    selectedList.source = "file";
    if (!selectedList.name.trim()) {
      selectedList.name = importState.fileName.replace(/\.[^.]+$/, "") || "Imported addresses";
    }
    if (!selectedList.notes.trim()) selectedList.notes = "Imported from file.";
    if (importSummary.value) workspace.notify(importSummary.value);
  }

  function appendImportedEntries(entries: AddressEntry[], warnings: ImportWarning[]) {
    const existingEmails = new Set(selectedList.entries.map((entry) => entry.email.trim().toLowerCase()));
    const nextEntries: AddressEntry[] = [];
    let skippedDuplicates = 0;
    for (const entry of entries) {
      const email = entry.email.trim().toLowerCase();
      if (existingEmails.has(email)) {
        skippedDuplicates += 1;
        if (warnings.length < MAX_IMPORT_WARNINGS) {
          warnings.push({
            row: 0,
            field: "email",
            message: `Imported email "${entry.email}" already exists and was skipped.`,
          });
        }
        continue;
      }
      existingEmails.add(email);
      nextEntries.push(entry);
    }
    return { entries: nextEntries, skippedDuplicates, warnings };
  }

  function ensureEntryFields(): void {
    for (const entry of selectedList.entries) {
      entry.fields = Object.fromEntries(selectedList.fields.map((field) => [
        field.key,
        field.role === "email" ? entry.email : addressFieldValue(entry.fields, field.key),
      ]));
    }
  }

  function refreshEntryDisplayNames(): void {
    const emailField = selectedList.fields.find((field) => field.role === "email");
    for (const entry of selectedList.entries) {
      if (emailField) entry.fields[emailField.key] = entry.email;
      entry.displayName = addressEntryDisplayName(entry.fields, entry.email, selectedList.fields);
    }
  }

  function updateEntryEmail(entry: AddressEntry, event: Event): void {
    const target = event.target;
    if (!(target instanceof HTMLInputElement)) return;
    entry.email = target.value;
    refreshEntryDisplayName(entry);
  }

  function updateEntryField(entry: AddressEntry, key: string, event: Event): void {
    const target = event.target;
    if (!(target instanceof HTMLInputElement)) return;
    entry.fields[key] = target.value;
    refreshEntryDisplayName(entry);
  }

  function refreshEntryDisplayName(entry: AddressEntry): void {
    const emailField = selectedList.fields.find((field) => field.role === "email");
    if (emailField) entry.fields[emailField.key] = entry.email;
    entry.displayName = addressEntryDisplayName(entry.fields, entry.email, selectedList.fields);
  }

  function clearImportState(): void {
    Object.assign(importState, {
      fileName: "",
      importedCount: 0,
      skippedDuplicateCount: 0,
      warnings: [],
      pending: null,
      mappingForm: emptyColumnMapping(),
    });
  }

  return {
    selectedList,
    importState,
    listSearch,
    entrySearch,
    selectedEntryKeys,
    listRows,
    entryRows,
    entryGridStyle,
    importSummary,
    omittedColumnCount,
    canAddCustomMapping,
    openNewAddressList,
    edit,
    addEntry,
    entryKey,
    deleteSelectedEntries,
    suppressSelectedEntries,
    selectAllEntries,
    openImportPicker,
    handleImportChange,
    mappingPreview,
    addCustomMapping,
    removeCustomMapping,
    updateMappingSource,
    cancelMapping,
    applyMapping,
    save,
    remove,
    exportList,
    updateEntryEmail,
    updateEntryField,
  };
}

function emptyAddressList(definitions: AddressFieldDefinition[]): EditableAddressList {
  return {
    id: 0,
    name: "",
    source: "manual",
    notes: "",
    fields: definitions.map((field) => ({ ...field })),
    entries: [],
    count: 0,
    createdAt: "",
    updatedAt: "",
  };
}

function emptyColumnMapping(): ColumnMapping {
  return { fields: [], headerLabels: [], suggestedEmailColumn: -1 };
}

function cloneColumnMapping(mapping: ColumnMapping): ColumnMapping {
  return {
    fields: mapping.fields.map((field) => ({ ...field })),
    headerLabels: [...mapping.headerLabels],
    suggestedEmailColumn: mapping.suggestedEmailColumn,
  };
}

function normalizeAddressEntry(entry: AddressEntry, definitions: AddressFieldDefinition[]): AddressEntry {
  const email = entry.email.trim();
  const fields = Object.fromEntries(definitions.map((field) => [
    field.key,
    field.role === "email" ? email : addressFieldValue(entry.fields, field.key).trim(),
  ]));
  return {
    id: entry.id || 0,
    email,
    displayName: addressEntryDisplayName(fields, email, definitions),
    fields,
  };
}

function addressEntryWritePayload(entry: AddressEntry, definitions: AddressFieldDefinition[]) {
  const normalized = normalizeAddressEntry(entry, definitions);
  return {
    email: normalized.email,
    fields: normalized.fields,
  };
}
