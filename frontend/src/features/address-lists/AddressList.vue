<script setup lang="ts">
import { nextTick, useTemplateRef } from "vue";
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";
import Table from "../../components/Table.vue";
import { addressFieldValue } from "../../import";
import { useAddressListsFeature } from "./useAddressLists";

const {
  selectedList,
  importState,
  entrySearch,
  selectedEntryKeys,
  entryRows,
  entryGridStyle,
  addEntry,
  deleteSelectedEntries,
  suppressSelectedEntries,
  handleImportChange,
  save,
  exportList,
  drafts,
  writing,
  updateEntry,
  cancelDraft,
} = useAddressListsFeature();

const importInput = useTemplateRef<HTMLInputElement>("importInput");
const table = useTemplateRef<HTMLElement>("table");

async function addAddress(): Promise<void> {
  addEntry();
  await nextTick();
  table.value?.querySelector<HTMLElement>(".data-table__row:last-child input[type=text]")?.focus();
}

</script>

<template>
  <section class="bulk-mail-section">
    <form class="app-form bulk-mail-form" @submit.prevent>
      <fieldset class="bulk-mail-fieldset">
        <label class="app-form-field">
          <span>List name</span>
          <input
            v-model="selectedList.name" type="text" placeholder="April launch audience"
            :disabled="writing" required @change="save()"
          />
        </label>
        <label class="app-form-field">
          <span>Notes</span>
          <textarea v-model="selectedList.notes" rows="3" :disabled="writing" @change="save()"></textarea>
        </label>
      </fieldset>
      <fieldset class="bulk-mail-fieldset">
        <legend>Addresses</legend>
        <input
          ref="importInput"
          class="visually-hidden"
          type="file"
          accept=".csv,.tsv,.xlsx,.vcf,.vcard"
          @change="handleImportChange"
        />
        <details v-if="importState.warnings.length > 0" class="bulk-mail-import-warnings">
          <summary>{{ importState.warnings.length }} import warnings</summary>
          <ul><li v-for="(warning, index) in importState.warnings" :key="index">{{ warning.message }}</li></ul>
        </details>
      </fieldset>

    </form>

    <div ref="table">
      <Table
        v-model:selected-keys="selectedEntryKeys"
        :rows="entryRows"
        :row-key="(row) => row.entry.id === null ? row.key : String(row.entry.id)"
        selectable
        :selection-disabled="writing"
        :is-row-selectable="(row) => !row.draft"
        :row-label="(row) => row.entry.fields.email"
        :row-class="(row) => [
          'data-table__row--entries',
          row.unsaved ? 'address-entry--unsaved' : '',
          row.draft?.rejected ? 'address-entry--rejected' : '',
        ].join(' ')"
        :row-style="entryGridStyle"
        header-class="data-table__row--entries data-table__row--field-header"
        empty-text="No addresses yet."
      >
        <template #actions>
          <PlusButton label="Add address" :disabled="writing" @click="addAddress" />
          <button
            type="button"
            class="data-table__action"
            :disabled="writing || drafts.length > 0"
            @click="importInput?.click()"
          >Import</button>
          <button
            type="button"
            class="data-table__action"
            :disabled="selectedList.entries.length === 0"
            @click="exportList"
          >
            Export
          </button>
          <button
            type="button"
            class="data-table__action"
            :disabled="writing || selectedEntryKeys.length === 0"
            @click="suppressSelectedEntries"
          >
            Suppress selected
          </button>
          <button
            type="button"
            class="data-table__action data-table__action--danger"
            :disabled="selectedEntryKeys.length === 0 || writing || drafts.length > 0"
            @click="deleteSelectedEntries"
          >
            Delete selected{{ selectedEntryKeys.length > 0 ? ' (' + selectedEntryKeys.length + ')' : '' }}
          </button>
        </template>
        <template #search>
          <ExpandableSearch v-model="entrySearch" label="Search addresses" />
        </template>
        <template #header>
          <div class="data-table__cell bulk-mail-field-heading">ID</div>
          <div
            v-for="field in selectedList.fields"
            :key="field.key"
            class="data-table__cell bulk-mail-field-heading"
          >
            {{ field.label }}
          </div>
        </template>
        <template #row="{ row }">
          <div class="data-table__cell" data-label="ID">{{ row.entry.id ?? "" }}</div>
          <div v-for="field in selectedList.fields" :key="field.key" class="data-table__cell" :data-label="field.label">
            <input
              :value="addressFieldValue(row.entry.fields, field.key)"
              type="text"
              :aria-label="field.label"
              :inputmode="field.role === 'email' ? 'email' : 'text'"
              :disabled="writing"
              @input="updateEntry(row.entry, field.key, $event)"
              @change="save(row.entry)"
              @keydown.esc="row.draft && cancelDraft(row.draft)"
            />
          </div>
        </template>
      </Table>
    </div>
  </section>
</template>

<style scoped>
:deep(.address-entry--unsaved) {
  --border-color: var(--warning-color);
}
:deep(.address-entry--rejected) {
  --border-color: var(--error-color);
}
:deep(.address-entry--unsaved input:focus-visible),
:deep(.address-entry--rejected input:focus-visible) {
  border-color: var(--border-color);
  outline: none;
  box-shadow: none;
}
</style>
