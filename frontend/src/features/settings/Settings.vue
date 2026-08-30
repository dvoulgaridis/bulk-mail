<script setup lang="ts">
import { useSettingsFeature } from "./useSettings";

const {
  draft,
  dependencies,
  dependenciesLoading,
  dependencyError,
  autoSaveState,
  autoSaveError,
} = useSettingsFeature();
</script>

<template>
  <section class="bulk-mail-section">
    <div class="app-form bulk-mail-form">
      <fieldset class="bulk-mail-fieldset" aria-label="Delivery and campaign settings">
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Emails rate (per minute)</span>
            <input
              v-model.number="draft.emailRatePerMin"
              type="number"
              min="1"
              max="10000"
              required
            />
          </label>
          <label class="app-form-field">
            <span>Emails interval (ms)</span>
            <input
              v-model.number="draft.emailIntervalMs"
              type="number"
              min="100"
              max="3600000"
              required
            />
          </label>
        </div>
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Maximum address entries</span>
            <input
              v-model.number="draft.maxCampaignAddressEntries"
              type="number"
              min="1"
              max="10000"
              required
            />
          </label>
          <label class="app-form-field">
            <span>Maximum attachments</span>
            <input
              v-model.number="draft.maxCampaignDocuments"
              type="number"
              min="1"
              max="5"
              required
            />
          </label>
        </div>
      </fieldset>
      <fieldset class="bulk-mail-fieldset">
        <legend>Local dependencies</legend>
        <div class="bulk-mail-dependency" aria-live="polite">
          <div class="bulk-mail-dependency-heading">
            <strong>LibreOffice</strong>
            <span v-if="dependencies" :data-state="dependencies.libreOffice.available ? 'success' : 'error'">
              {{ dependencies.libreOffice.available ? "Available" : "Unavailable" }}
            </span>
          </div>
          <p v-if="dependenciesLoading" class="app-form-help">Checking LibreOffice...</p>
          <p v-else-if="dependencyError" class="app-form-status" data-state="error">{{ dependencyError }}</p>
          <template v-else-if="dependencies">
            <p class="app-form-status" :data-state="dependencies.libreOffice.available ? 'success' : 'error'">
              {{ dependencies.libreOffice.version || dependencies.libreOffice.error || "No version information." }}
            </p>
            <p class="bulk-mail-dependency-path">
              <span>Path</span>
              <code>{{ dependencies.libreOffice.path || "N/A" }}</code>
            </p>
          </template>
        </div>
      </fieldset>
      <p v-if="autoSaveState === 'error'" class="app-form-status" data-state="error">{{ autoSaveError }}</p>
    </div>
  </section>
</template>
