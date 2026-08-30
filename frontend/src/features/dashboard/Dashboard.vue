<script setup lang="ts">
import { computed } from "vue";
import { useWorkspace } from "../../app/context";
import { useReportsFeature } from "../reports/useReports";

const { state, navigate } = useWorkspace();
const { taskRows, openTaskReport } = useReportsFeature();
const counts = computed(() => ({
  profiles: state.smtpProfiles.length,
  lists: state.addressLists.length,
  campaigns: state.campaigns.length,
  suppressions: state.suppressions.length,
}));
</script>

<template>
  <section class="bulk-mail-section">
    <div class="app-tile-grid">
      <button type="button" class="app-tile" @click="navigate('profiles')">
        <span>PROFILES</span>
        <div class="app-tile-value"><strong>{{ counts.profiles }}</strong></div>
      </button>
      <button type="button" class="app-tile" @click="navigate('address-lists')">
        <span>ADDRESS LISTS</span>
        <div class="app-tile-value"><strong>{{ counts.lists }}</strong></div>
      </button>
      <button type="button" class="app-tile" @click="navigate('campaigns')">
        <span>CAMPAIGNS</span>
        <div class="app-tile-value"><strong>{{ counts.campaigns }}</strong></div>
      </button>
      <button type="button" class="app-tile" @click="navigate('suppressions')">
        <span>SUPPRESSIONS</span>
        <div class="app-tile-value"><strong>{{ counts.suppressions }}</strong></div>
      </button>
    </div>
  </section>

  <section class="bulk-mail-section">
    <div class="bulk-mail-section-head"><h3>Tasks</h3></div>
    <div class="data-table">
      <div class="data-table__viewport">
        <div class="data-table__row data-table__row--header data-table__row--tasks" role="row">
          <div class="data-table__cell">Date</div>
          <div class="data-table__cell">Campaign</div>
          <div class="data-table__cell data-table__cell--right">Progress</div>
          <div class="data-table__cell data-table__cell--right">Failed</div>
          <div class="data-table__cell data-table__cell--right">Skipped</div>
          <div class="data-table__cell">Status</div>
        </div>
        <div v-if="taskRows.length === 0" class="data-table__empty">No tasks yet.</div>
        <button v-for="row in taskRows.slice(0, 5)" v-else :key="row.id" type="button" class="data-table__row data-table__row--link data-table__row--tasks" role="row" @click="openTaskReport(row.id)">
          <div class="data-table__cell data-table__cell--truncate" data-label="Date">{{ row.date }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Campaign">{{ row.campaign }}</div>
          <div class="data-table__cell data-table__cell--right" data-label="Progress">{{ row.progress }}</div>
          <div class="data-table__cell data-table__cell--right" data-label="Failed">{{ row.failed }}</div>
          <div class="data-table__cell data-table__cell--right" data-label="Skipped">{{ row.skipped }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Status">{{ row.status }}</div>
        </button>
      </div>
    </div>
  </section>
</template>
