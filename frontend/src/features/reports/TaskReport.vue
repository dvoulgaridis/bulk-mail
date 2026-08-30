<script setup lang="ts">
import { computed } from "vue";
import { useWorkspace } from "../../app/context";
import { shortDate } from "../../common/format";
import { useReportsFeature } from "./useReports";

const { state } = useWorkspace();
const {
  selectedTaskReport,
  selectedTaskActive,
  cancelSelectedTask,
  downloadArchive,
  exportResults,
  suppressEmailAddress,
} = useReportsFeature();
const suppressionEmails = computed(() => new Set(state.suppressions.map((item) => item.email)));
const retryableCount = computed(
  () =>
    selectedTaskReport.value?.deliveries.filter(
      (item) => !["sent", "generated"].includes(item.status),
    ).length || 0,
);
</script>

<template>
  <section v-if="selectedTaskReport" class="bulk-mail-report-panel">
    <div class="bulk-mail-section-head">
      <div>
        <h3>Campaign report</h3>
        <p>Task {{ selectedTaskReport.task.id }} · {{ selectedTaskReport.metadata.mode || "send" }}</p>
      </div>
      <span>{{ selectedTaskReport.task.status }}</span>
    </div>

    <dl class="bulk-mail-report-metadata">
      <div><dt>Address list</dt><dd>{{ selectedTaskReport.metadata.listName || "Unavailable" }}</dd></div>
      <div><dt>Profile</dt><dd>{{ selectedTaskReport.metadata.profileName || "Not used" }}</dd></div>
      <div><dt>Attachments</dt><dd>{{ selectedTaskReport.metadata.attachmentNames?.join(", ") || "None" }}</dd></div>
      <div><dt>Started</dt><dd>{{ shortDate(selectedTaskReport.task.createdAt) }}</dd></div>
      <div><dt>Finished / updated</dt><dd>{{ shortDate(selectedTaskReport.task.updatedAt) }}</dd></div>
    </dl>

    <div class="bulk-mail-report-summary">
      <span><strong>{{ selectedTaskReport.task.sent }}</strong> sent</span>
      <span><strong>{{ selectedTaskReport.task.failed }}</strong> failed</span>
      <span><strong>{{ selectedTaskReport.task.skipped }}</strong> skipped</span>
      <span><strong>{{ selectedTaskReport.task.total }}</strong> total</span>
    </div>
    <div class="bulk-mail-report-summary">
      <span v-for="(count, status) in selectedTaskReport.statusCounts" :key="status">
        <strong>{{ count }}</strong> {{ status }}
      </span>
    </div>
    <div v-if="selectedTaskReport.failures.length" class="bulk-mail-report-summary">
      <span v-for="failure in selectedTaskReport.failures" :key="failure.category">
        <strong>{{ failure.count }}</strong>
        {{ failure.category }} failure{{ failure.count === 1 ? "" : "s" }}
      </span>
    </div>

    <div class="app-stage-actions app-stage-actions--start">
      <button v-if="selectedTaskActive" type="button" class="is-danger" @click="cancelSelectedTask">Cancel task</button>
      <button
        v-if="selectedTaskReport.archiveAvailable"
        type="button"
        @click="downloadArchive"
      >
        Download archive
      </button>
      <button type="button" @click="exportResults(false)">Export visible results</button>
      <button type="button" :disabled="retryableCount === 0" @click="exportResults(true)">
        Export failed / unsent
      </button>
    </div>
    <p
      v-if="
        selectedTaskReport.metadata.mode === 'generate' &&
        ['completed', 'completed_with_errors'].includes(selectedTaskReport.task.status) &&
        !selectedTaskReport.archiveAvailable
      "
      class="app-form-help"
    >The archive is no longer available. Queue a new generation task to create it again.</p>
    <p v-if="retryableCount" class="app-form-help">
      To retry, import the failed / unsent CSV into a new address list and create a new
      campaign. Nothing is resent automatically.
    </p>

    <div class="data-table">
      <div class="data-table__viewport">
        <div class="data-table__row data-table__row--header data-table__row--deliveries" role="row">
          <div class="data-table__cell">Email</div>
          <div class="data-table__cell">Status</div>
          <div class="data-table__cell">Attempts / provider ID</div>
          <div class="data-table__cell">Result</div>
          <div class="data-table__cell"></div>
        </div>
        <div v-if="selectedTaskReport.deliveries.length === 0" class="data-table__empty">
          No delivery outcomes yet.
        </div>
        <div
          v-for="delivery in selectedTaskReport.deliveries"
          v-else
          :key="delivery.id"
          class="data-table__row data-table__row--deliveries"
          role="row"
        >
          <div class="data-table__cell data-table__cell--truncate" data-label="Email">{{ delivery.email }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Status">{{ delivery.status }}</div>
          <div
            class="data-table__cell data-table__cell--truncate"
            data-label="Attempts / provider ID"
          >
            {{ delivery.attempt }} · {{ delivery.providerMessageId || "n/a" }}
          </div>
          <div
            class="data-table__cell data-table__cell--truncate"
            data-label="Result"
            :title="delivery.lastError"
          >
            {{
              delivery.lastError ||
              (delivery.status === "generated" ? "Generated" : "Sent")
            }}
          </div>
          <div class="data-table__cell" data-label="Suppression">
            <button
              v-if="!suppressionEmails.has(delivery.email)"
              type="button"
              class="data-table__action"
              @click="suppressEmailAddress(delivery.email)"
            >
              Suppress
            </button>
            <span v-else class="app-form-help">Suppressed</span>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
