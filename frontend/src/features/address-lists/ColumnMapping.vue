<script setup lang="ts">
import { computed, nextTick, ref } from "vue";
import { placeholderToken } from "../../common/format";
import PlusButton from "../../components/PlusButton.vue";
import {
  MAX_ADDRESS_FIELD_CHARACTERS,
  MAX_PLACEHOLDER_KEY_CHARACTERS,
} from "../../import";
import { useAddressListsFeature } from "./useAddressLists";

const {
  importState,
  omittedColumnCount,
  canAddCustomMapping,
  mappingPreview,
  addCustomMapping,
  removeCustomMapping,
  updateMappingSource,
  cancelMapping,
  applyMapping,
} = useAddressListsFeature();

const emailMapping = computed(() => importState.mappingForm.fields.find((field) => field.role === "email"));
const mappingList = ref<HTMLElement | null>(null);

function sourceOptionDisabled(sourceIndex: number, fieldIndex: number): boolean {
  return importState.mappingForm.fields.some(
    (field, index) => index !== fieldIndex && field.sourceIndex === sourceIndex,
  );
}

async function addMapping(): Promise<void> {
  const previousCount = importState.mappingForm.fields.length;
  addCustomMapping();
  if (importState.mappingForm.fields.length === previousCount) return;
  await nextTick();
  const row = mappingList.value?.lastElementChild;
  const incomplete = row?.querySelector<HTMLElement>("input:invalid, select:invalid");
  (incomplete ?? row?.querySelector<HTMLElement>("input, select"))?.focus();
}
</script>

<template>
  <section class="bulk-mail-section">
    <form v-if="importState.pending" class="app-form bulk-mail-form" @submit.prevent="applyMapping">
      <fieldset class="bulk-mail-fieldset">
        <legend>Column mapping</legend>
        <p class="bulk-mail-import-heading">Choose address fields for {{ importState.pending.fileName }}.</p>
        <p class="app-form-help">Email is required. First name and last name are optional.</p>

        <div ref="mappingList" class="bulk-mail-mapping-list">
          <div
            v-for="(field, fieldIndex) in importState.mappingForm.fields"
            :key="fieldIndex"
            class="bulk-mail-mapping-row"
          >
            <div class="bulk-mail-mapping-definition">
              <template v-if="field.origin === 'new'">
                <label class="app-form-field">
                  <span>Field name</span>
                  <input
                    v-model="field.label"
                    type="text"
                    :maxlength="MAX_ADDRESS_FIELD_CHARACTERS"
                    required
                  />
                </label>
                <label class="app-form-field">
                  <span>Placeholder</span>
                  <input
                    v-model="field.key"
                    type="text"
                    :maxlength="MAX_PLACEHOLDER_KEY_CHARACTERS"
                    required
                  />
                </label>
              </template>
              <template v-else>
                <strong>{{ field.label }}</strong>
                <code v-if="field.origin === 'persisted'">{{ placeholderToken(field.key) }}</code>
              </template>
            </div>
            <label class="app-form-field">
              <span>Source column</span>
              <select
                v-model.number="field.sourceIndex"
                :required="field.role === 'email' || field.origin === 'new'"
                @change="updateMappingSource(field)"
              >
                <option :disabled="field.role === 'email' || field.origin === 'new'" :value="-1">
                  {{ field.role === "email" || field.origin === "new" ? "Select column" : "Do not import" }}
                </option>
                <option
                  v-for="(label, sourceIndex) in importState.pending.columnMapping.headerLabels"
                  :key="sourceIndex"
                  :value="sourceIndex"
                  :disabled="sourceOptionDisabled(sourceIndex, fieldIndex)"
                >
                  {{ label }}{{
                    sourceIndex === importState.pending.columnMapping.suggestedEmailColumn
                      ? " (email suggested)"
                      : ""
                  }}
                </option>
              </select>
            </label>

            <div class="bulk-mail-mapping-preview">
              <span>Preview</span>
              <small>{{ mappingPreview(field) }}</small>
            </div>

            <button
              v-if="field.origin === 'new'"
              type="button"
              class="header-add-button bulk-mail-remove-mapping"
              aria-label="Remove custom mapping"
              title="Remove custom mapping"
              @click="removeCustomMapping(fieldIndex)"
            >
              <span aria-hidden="true">−</span>
            </button>
            <span v-else-if="field.role === 'email'" class="bulk-mail-required-field">Required</span>
          </div>
        </div>

        <div class="bulk-mail-mapping-summary">
          <span>{{ omittedColumnCount }} source column{{ omittedColumnCount === 1 ? "" : "s" }} not included</span>
          <PlusButton
            label="Add custom mapping"
            :disabled="!canAddCustomMapping"
            @click="addMapping"
          />
        </div>

        <p
          v-if="
            importState.pending.columnMapping.suggestedEmailColumn >= 0
              && emailMapping
              && emailMapping.sourceIndex < 0
          "
          class="app-form-status"
        >
          {{
            importState.pending.columnMapping.headerLabels[
              importState.pending.columnMapping.suggestedEmailColumn
            ]
          }}
          appears to contain email addresses. Confirm it in the address email row.
        </p>

        <div class="app-stage-actions">
          <button type="button" @click="cancelMapping">Cancel</button>
          <button type="submit" class="is-primary">Apply mapping</button>
        </div>
      </fieldset>
    </form>
  </section>
</template>
