<script setup lang="ts">
import { useWorkspace } from "../../app/context";
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";
import TaskReport from "../reports/TaskReport.vue";
import { useReportsFeature } from "../reports/useReports";
import { useCampaignsFeature } from "./useCampaigns";

const { state } = useWorkspace();
const { openNewCampaign, editCampaign } = useCampaignsFeature();
const {
  search,
  selectedTaskID,
  filteredTaskRows,
  selectedTaskRow,
  selectedTaskActive,
  selectTask,
  viewSelectedTaskReport,
  cancelSelectedTask,
} = useReportsFeature();

function openCampaign(event: Event): void {
  const target = event.target;
  if (!(target instanceof HTMLSelectElement)) return;
  const id = Number(target.value);
  target.value = "0";
  if (id > 0) void editCampaign(id);
}
</script>

<template>
  <section class="bulk-mail-section">
    <div class="data-table">
      <div class="data-table__toolbar">
        <div class="data-table__toolbar-start">
          <PlusButton label="New campaign" @click="openNewCampaign" />
          <select aria-label="Open saved campaign" :value="0" @change="openCampaign">
            <option :value="0">Open saved campaign</option>
            <option v-for="campaign in state.campaigns" :key="campaign.id" :value="campaign.id">
              {{ campaign.name }}
            </option>
          </select>
        </div>
        <ExpandableSearch v-model="search" label="Search campaigns" />
      </div>
      <div class="data-table__viewport data-table__viewport--campaigns">
        <div class="data-table__row data-table__row--header data-table__row--tasks" role="row">
          <div class="data-table__cell">Date</div>
          <div class="data-table__cell">Campaign</div>
          <div class="data-table__cell data-table__cell--right">Progress</div>
          <div class="data-table__cell data-table__cell--right">Failed</div>
          <div class="data-table__cell data-table__cell--right">Skipped</div>
          <div class="data-table__cell">Status</div>
        </div>
        <div v-if="filteredTaskRows.length === 0" class="data-table__empty">No tasks yet.</div>
        <button
          v-for="row in filteredTaskRows"
          v-else
          :key="row.id"
          type="button"
          class="data-table__row data-table__row--link data-table__row--tasks"
          role="row"
          :data-active="selectedTaskID === row.id || undefined"
          @click="selectTask(row.id)"
        >
          <div class="data-table__cell data-table__cell--truncate" data-label="Date">{{ row.date }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Campaign">{{ row.campaign }}</div>
          <div class="data-table__cell data-table__cell--right" data-label="Progress">{{ row.progress }}</div>
          <div class="data-table__cell data-table__cell--right" data-label="Failed">{{ row.failed }}</div>
          <div class="data-table__cell data-table__cell--right" data-label="Skipped">{{ row.skipped }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Status">{{ row.status }}</div>
        </button>
      </div>
    </div>
    <div v-if="selectedTaskRow" class="app-stage-actions app-stage-actions--start">
      <span class="app-stage-note">Selected {{ selectedTaskRow.campaign }}</span>
      <button type="button" @click="viewSelectedTaskReport">View report</button>
      <button v-if="selectedTaskActive" type="button" class="is-danger" @click="cancelSelectedTask">Cancel task</button>
    </div>
    <TaskReport />
  </section>
</template>
