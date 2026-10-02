import {
  computed,
  inject,
  provide,
  reactive,
  ref,
  type InjectionKey,
} from "vue";
import type {
  AddressEntry,
  EntryWriteResult,
  AddressFieldDefinition,
  AddressList,
} from "../../api/types";
import type { WorkspaceContext } from "../../app/context";
import { saveTextFile } from "../../common/files";
import { shortDate } from "../../common/format";
import { entryWriteSucceeded, notifyEntryWrites } from "../../common/entryWrites";
import { exportAddressListAsCSV, exportAddressListAsVCard } from "../../import/export";
import {
  MAX_IMPORT_WARNINGS,
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

type EntryDraft = {
  key: number;
  entry: AddressEntry;
  rejected: boolean;
};

type PendingImport = {
  fileName: string;
  rows: string[][];
  warnings: ImportWarning[];
  columnMapping: ColumnMapping;
};

type ImportState = {
  fileName: string;
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
  const listSearch = ref("");
  const entrySearch = ref("");
  const drafts = ref<EntryDraft[]>([]);
  const writing = ref(false);
  let nextDraftKey = 0;
  const selectedEntryKeys = ref<string[]>([]);
  const selectedListKeys = ref<string[]>([]);
  const selectedList = reactive<EditableAddressList>(emptyAddressList(workspace.state.addressFieldDefaults));
  let savedMetadata = metadataKey(selectedList);
  const importState = reactive<ImportState>({
    fileName: "",
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
    const editable = new Map(drafts.value
      .filter((draft) => draft.entry.id !== null)
      .map((draft) => [draft.entry.id, draft]));
    const rows = selectedList.entries.map((entry) => {
      const draft = editable.get(entry.id);
      const unsaved = draft ? Object.keys({ ...entry.fields, ...draft.entry.fields }).some((key) =>
        (entry.fields[key] || "") !== (draft.entry.fields[key] || ""),
      ) : false;
      return { entry: draft?.entry || entry, draft, unsaved, key: `saved-${entry.id}` };
    });
    const displayed = new Set(rows.map((row) => row.draft?.key));
    for (const draft of drafts.value) {
      if (!displayed.has(draft.key)) {
        rows.push({ entry: draft.entry, draft, unsaved: true, key: `draft-${draft.key}` });
      }
    }
    return rows.filter(({ entry, draft }) => draft || !query ||
      Object.values(entry.fields).some((value) => value.toLowerCase().includes(query)),
    );
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
    gridTemplateColumns: `repeat(2, minmax(3rem, 4rem)) repeat(${Math.max(selectedList.fields.length, 1)}, minmax(11rem, 1fr))`,
  }));

  function openNewAddressList(): void {
    if (writing.value) return;
    Object.assign(selectedList, emptyAddressList(workspace.state.addressFieldDefaults));
    savedMetadata = metadataKey(selectedList);
    drafts.value = [];
    clearImportState();
    selectedEntryKeys.value = [];
    workspace.navigate("address-list-detail");
  }

  async function edit(id: number): Promise<void> {
    if (writing.value) return;
    await workspace.runAction(async () => {
      drafts.value = [];
      clearImportState();
      await requestAddressList(id);
      workspace.navigate("address-list-detail");
    });
  }

  async function requestAddressList(id: number, keepMetadataDraft = false): Promise<void> {
    const addressList = await workspace.api.request<AddressList>(`/api/address-lists/${id}`);
    savedMetadata = metadataKey(addressList);
    const metadataDraft = keepMetadataDraft
      ? { name: selectedList.name, notes: selectedList.notes, source: selectedList.source }
      : {};
    Object.assign(selectedList, addressList, { entries: addressList.entries || [] }, metadataDraft);
    selectedEntryKeys.value = [];
  }

  function addEntry(): void {
    if (writing.value) return;
    drafts.value.push({
      key: ++nextDraftKey,
      entry: createAddressListEntry("", selectedList.fields),
      rejected: false,
    });
  }

  function entryKey(entry: AddressEntry): string {
    return String(entry.id);
  }

  function updateEntry(entry: AddressEntry, field: string, event: Event): void {
    if (writing.value || !(event.target instanceof HTMLInputElement)) return;
    if (entry.fields[field] === event.target.value) return;
    let draft = drafts.value.find((item) => item.entry === entry || (
      entry.id !== null && item.entry.id === entry.id
    ));
    if (!draft) {
      draft = {
        key: ++nextDraftKey,
        entry: { ...entry, fields: { ...entry.fields } },
        rejected: false,
      };
      drafts.value.push(draft);
    }
    draft.entry.fields[field] = event.target.value;
    draft.rejected = false;
    if (!entryRows.value.some((row) => row.draft?.key === draft.key && row.unsaved)) cancelDraft(draft);
  }

  function cancelDraft(draft: EntryDraft): void {
    if (!writing.value) drafts.value = drafts.value.filter((item) => item.key !== draft.key);
  }

  async function write(action: () => Promise<void>): Promise<boolean> {
    if (writing.value) return false;
    let completed = false;
    writing.value = true;
    try {
      await workspace.runAction(async () => {
        try {
          await action();
          completed = true;
        }
        catch (error) {
          // Refresh persisted rows while retaining drafts after a rejected or uncertain write.
          try {
            if (selectedList.id) await requestAddressList(selectedList.id, true);
            await workspace.refresh();
          }
          finally { throw error; }
        }
      });
    }
    finally { writing.value = false; }
    return completed;
  }

  async function deleteSelectedEntries(): Promise<void> {
    if (drafts.value.length || !selectedEntryKeys.value.length) return;
    await write(async () => {
      const result = await workspace.api.request<EntryWriteResult>(
        `/api/address-lists/${selectedList.id}/entries/delete`,
        { method: "POST", body: { ids: selectedEntryKeys.value.map(Number) } },
      );
      await requestAddressList(selectedList.id, true);
      await workspace.refresh();
      notifyEntryWrites(workspace, result);
    });
  }

  async function deleteSelectedLists(): Promise<void> {
    if (writing.value || selectedListKeys.value.length === 0) return;
    const selected = new Set(selectedListKeys.value);
    const lists = workspace.state.addressLists.filter((list) => selected.has(String(list.id)));
    if (!lists.length || !window.confirm(
      `Delete ${lists.length} selected address list(s) and all their entries?`,
    )) return;
    writing.value = true;
    try {
      await workspace.runAction(async () => {
        let deleted = 0;
        const failures: string[] = [];
        for (const list of lists) {
          try {
            await workspace.api.request<void>(`/api/address-lists/${list.id}`, { method: "DELETE" });
            selectedListKeys.value = selectedListKeys.value.filter((key) => key !== String(list.id));
            deleted++;
          } catch (error) {
            failures.push(`${list.name}: ${error instanceof Error ? error.message : String(error)}`);
          }
        }
        await workspace.refresh();
        if (deleted) workspace.notify(`${deleted} address list(s) deleted.`);
        if (failures.length) workspace.notify(failures.join("\n"), "error");
      });
    } finally {
      writing.value = false;
    }
  }

  async function suppressSelectedEntries(): Promise<void> {
    if (selectedEntryKeys.value.length === 0) return;
    const selected = new Set(selectedEntryKeys.value);
    const emails = selectedList.entries
      .filter((entry) => selected.has(entryKey(entry)))
      .map((entry) => entry.fields.email);
    if (emails.length === 0) {
      workspace.notify("Selected rows do not contain email addresses.", "error");
      return;
    }
    await write(async () => {
      const result = await workspace.api.request<EntryWriteResult>("/api/suppressions", {
        method: "POST", body: { emails, reason: "address list" },
      });
      await workspace.requestSuppressions();
      notifyEntryWrites(workspace, result);
    });
  }

  async function handleImportChange(event: Event): Promise<void> {
    if (writing.value) return;
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importState.fileName = file.name;
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
      } else await requestImport(result);
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

  async function applyMapping(): Promise<void> {
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
      if (!await requestImport(result)) return;
      importState.pending = null;
      workspace.navigate("address-list-detail");
    } catch (error) {
      workspace.notify(error instanceof Error ? error.message : String(error), "error");
    }
  }

  // Metadata and entries are separate writes; only GET populates persisted rows.
  async function requestSaveMetadata(): Promise<void> {
    if (selectedList.id && metadataKey(selectedList) === savedMetadata) return;
    const saved = await workspace.api.request<{ id: number }>(
      selectedList.id ? `/api/address-lists/${selectedList.id}` : "/api/address-lists",
      {
        method: selectedList.id ? "PUT" : "POST",
        body: {
          name: selectedList.name, source: selectedList.source, notes: selectedList.notes,
          fields: selectedList.fields,
        },
      },
    );
    await requestAddressList(saved.id);
    await workspace.refresh();
  }

  async function requestEntryWrites(
    pending: EntryDraft[], operation: "insert" | "update", fields?: AddressFieldDefinition[],
  ): Promise<void> {
    if (!pending.length) return;
    const result = await workspace.api.request<EntryWriteResult>(
      `/api/address-lists/${selectedList.id}/entries${fields ? "/import" : ""}`,
      {
        method: operation === "update" ? "PATCH" : "POST",
        body: {
          fields,
          entries: pending.map(({ entry }) => operation === "insert"
            ? { fields: entry.fields }
            : { id: entry.id, fields: entry.fields }),
        },
      },
    );
    const accepted = new Set<number>();
    for (const outcome of result.results) {
      const draft = pending[outcome.index];
      if (!draft) continue;
      if (entryWriteSucceeded(outcome)) {
        accepted.add(draft.key);
      } else draft.rejected = outcome.status !== "not_processed";
    }
    drafts.value = drafts.value.filter((draft) => !accepted.has(draft.key));
    if (result.stopped) throw new Error(result.stopped);
    await requestAddressList(selectedList.id, true);
    await workspace.refresh();
    notifyEntryWrites(workspace, result);
  }

  async function save(entry?: AddressEntry): Promise<void> {
    if (writing.value) return;
    const draft = entry && drafts.value.find((item) => item.entry === entry);
    if (entry && !draft) return;
    if (!draft && metadataKey(selectedList) === savedMetadata) return;
    await write(async () => {
      await requestSaveMetadata();
      if (draft) await requestEntryWrites([draft], draft.entry.id === null ? "insert" : "update");
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

  async function requestImport(result: ImportResult): Promise<boolean> {
    if (!result.fields) throw new Error("The imported address fields are unavailable.");
    return write(async () => {
      if (!selectedList.name) {
        selectedList.name = importState.fileName.replace(/\.[^.]+$/, "") || "Imported addresses";
      }
      selectedList.source = "file";
      await requestSaveMetadata();
      const pending = result.entries.map((entry) => ({
        key: ++nextDraftKey, entry, rejected: false,
      }));
      drafts.value.push(...pending);
      await requestEntryWrites(pending, "insert", result.fields);
      importState.warnings = result.warnings;
    });
  }

  function clearImportState(): void {
    Object.assign(importState, {
      fileName: "",
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
    selectedListKeys,
    listRows,
    entryRows,
    entryGridStyle,
    omittedColumnCount,
    canAddCustomMapping,
    openNewAddressList,
    edit,
    addEntry,
    deleteSelectedEntries,
    deleteSelectedLists,
    suppressSelectedEntries,
    handleImportChange,
    mappingPreview,
    addCustomMapping,
    removeCustomMapping,
    updateMappingSource,
    cancelMapping,
    applyMapping,
    save,
    exportList,
    drafts,
    writing,
    updateEntry,
    cancelDraft,
  };
}

function metadataKey(list: AddressList): string {
  return JSON.stringify([list.name, list.notes, list.source]);
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
