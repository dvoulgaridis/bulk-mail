<script setup lang="ts">
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";
import { addressFieldValue } from "../../import";
import { useAddressListsFeature } from "./useAddressLists";

const {
  selectedList,
  importState,
  importSummary,
  entrySearch,
  selectedEntryKeys,
  entryRows,
  entryGridStyle,
  addEntry,
  entryKey,
  deleteSelectedEntries,
  suppressSelectedEntries,
  selectAllEntries,
  openImportPicker,
  handleImportChange,
  save,
  remove,
  exportList,
  updateEntryEmail,
  updateEntryField,
} = useAddressListsFeature();
</script>

<template>
  <section class="bulk-mail-section">
    <form id="bulk-mail-address-list-form" class="app-form bulk-mail-form" @submit.prevent="save">
      <fieldset class="bulk-mail-fieldset">
        <label class="app-form-field">
          <span>List name</span>
          <input v-model="selectedList.name" type="text" placeholder="April launch audience" required />
        </label>
        <label class="app-form-field">
          <span>Notes</span>
          <textarea v-model="selectedList.notes" rows="3"></textarea>
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
        <p v-if="importSummary" class="app-form-status" data-state="success">{{ importSummary }}</p>
        <details v-if="importState.warnings.length > 0" class="bulk-mail-import-warnings">
          <summary>{{ importState.warnings.length }} import warnings</summary>
          <ul><li v-for="(warning, index) in importState.warnings" :key="index">{{ warning.message }}</li></ul>
        </details>
      </fieldset>

    </form>

    <div class="data-table">
      <div class="data-table__toolbar">
        <div class="data-table__toolbar-start">
          <PlusButton label="Add address" @click="addEntry" />
          <button type="button" class="data-table__action" @click="openImportPicker">Import</button>
          <button
            type="button"
            class="data-table__action"
            :disabled="selectedList.entries.length === 0"
            @click="selectAllEntries"
          >
            Select all
          </button>
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
            :disabled="selectedEntryKeys.length === 0"
            @click="suppressSelectedEntries"
          >
            Suppress selected
          </button>
          <button
            type="button"
            class="data-table__action data-table__action--danger"
            :disabled="selectedEntryKeys.length === 0"
            @click="deleteSelectedEntries"
          >
            Delete selected{{ selectedEntryKeys.length > 0 ? ' (' + selectedEntryKeys.length + ')' : '' }}
          </button>
        </div>
        <ExpandableSearch v-model="entrySearch" label="Search addresses" />
      </div>
      <div class="data-table__viewport">
        <div
          class="data-table__row data-table__row--header data-table__row--entries data-table__row--field-header"
          :style="entryGridStyle"
          role="row"
        >
          <div class="data-table__cell"></div>
          <div
            v-for="field in selectedList.fields"
            :key="field.key"
            class="data-table__cell bulk-mail-field-heading"
          >
            {{ field.label }}
          </div>
        </div>
        <div v-if="entryRows.length === 0" class="data-table__empty">No addresses yet.</div>
        <div
          v-for="row in entryRows"
          v-else
          :key="entryKey(row.entry, row.index)"
          class="data-table__row data-table__row--entries"
          :style="entryGridStyle"
          role="row"
        >
          <div class="data-table__cell data-table__cell--select" data-label="Select">
            <input v-model="selectedEntryKeys" type="checkbox" :value="entryKey(row.entry, row.index)" />
          </div>
          <div v-for="field in selectedList.fields" :key="field.key" class="data-table__cell" :data-label="field.label">
            <input
              v-if="field.role === 'email'"
              :value="row.entry.email"
              type="email"
              placeholder="name@example.com"
              required
              form="bulk-mail-address-list-form"
              @input="updateEntryEmail(row.entry, $event)"
            />
            <input
              v-else
              :value="addressFieldValue(row.entry.fields, field.key)"
              type="text"
              :placeholder="field.label"
              form="bulk-mail-address-list-form"
              @input="updateEntryField(row.entry, field.key, $event)"
            />
          </div>
        </div>
      </div>
    </div>
    <div class="app-stage-actions">
      <button v-if="selectedList.id" type="button" @click="remove">Delete</button>
      <button type="submit" class="is-primary" form="bulk-mail-address-list-form">
        {{ selectedList.id ? "Save address list" : "Create address list" }}
      </button>
    </div>
  </section>
</template>
