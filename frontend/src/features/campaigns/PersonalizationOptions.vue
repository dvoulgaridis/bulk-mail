<script setup lang="ts">
import type { PlaceholderOptions } from "../../api/types";

defineProps<{ options: PlaceholderOptions }>();

const nameFormats = [
  { key: "firstNameFormat", label: "First name format" },
  { key: "lastNameFormat", label: "Last name format" },
  { key: "fullNameFormat", label: "Full name format" },
] as const;
</script>

<template>
  <details class="personalization-options">
    <summary>Personalization</summary>
    <div class="personalization-controls">
      <label class="app-checkbox-field">
        <input v-model="options.substitutePlaceholders" type="checkbox" />
        <span>Substitute placeholders</span>
      </label>
      <fieldset :disabled="!options.substitutePlaceholders">
        <label class="app-checkbox-field">
          <input v-model="options.removeDiacritics" type="checkbox" />
          <span>Remove diacritics</span>
        </label>
        <div class="app-form-three-up">
          <label v-for="field in nameFormats" :key="field.key" class="app-form-field">
            <span>{{ field.label }}</span>
            <select v-model="options[field.key]">
              <option value="preserve">Preserve</option>
              <option value="upper">Uppercase</option>
              <option value="title">Title case</option>
            </select>
          </label>
        </div>
      </fieldset>
      <slot />
    </div>
  </details>
</template>

<style scoped>
summary {
  cursor: pointer;
  font-weight: 700;
}

.personalization-controls,
fieldset {
  display: grid;
  gap: 1rem;
}

.personalization-controls {
  margin-top: 1rem;
}

fieldset {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
</style>
