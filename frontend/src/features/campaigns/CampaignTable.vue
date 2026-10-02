<script setup lang="ts">
import { computed, ref } from "vue";
import { useWorkspace } from "../../app/context";
import { shortDate } from "../../common/format";
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import Table from "../../components/Table.vue";
import { useReportsFeature } from "../reports/useReports";
import { useCampaignsFeature } from "./useCampaigns";

const props = defineProps<{ limit?: number }>();
const workspace = useWorkspace();
const { editCampaign } = useCampaignsFeature();
const { selectedTaskID, openTaskReport } = useReportsFeature();
const search = ref("");
const statusLabels: Record<string, string> = {
  not_run: "Not run",
  queued: "Queued",
  preparing: "Preparing",
  running: "Running",
  completed: "Completed",
  completed_with_errors: "Completed with errors",
  cancelled: "Cancelled",
  interrupted: "Interrupted",
};

const rows = computed(() => {
  const executedCampaigns = new Set(workspace.tasks.items.map((task) => task.campaignId));
  const history = workspace.tasks.items.map((task) => ({
    key: `task:${task.id}`,
    id: task.id,
    taskID: task.id,
    campaignID: task.campaignId,
    createdAt: task.createdAt,
    campaign: task.campaignName || "Unnamed campaign",
    progress: `${Math.min(task.sent + task.failed + task.skipped, task.total)}/${task.total}`,
    failed: String(task.failed),
    skipped: String(task.skipped || 0),
    status: statusLabels[task.status] || task.status,
    lastError: task.lastError || "",
  }));
  const unrun = workspace.state.campaigns
    .filter((campaign) => !executedCampaigns.has(campaign.id))
    .map((campaign) => ({
      key: `campaign:${campaign.id}`,
      id: campaign.id,
      taskID: null,
      campaignID: campaign.id,
      createdAt: campaign.createdAt,
      campaign: campaign.name || "Unnamed campaign",
      progress: "—",
      failed: "—",
      skipped: "—",
      status: statusLabels.not_run,
      lastError: "",
    }));
  const query = search.value.trim().toLowerCase();
  const result = [...history, ...unrun]
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt) || right.id - left.id)
    .map((row) => ({ ...row, date: shortDate(row.createdAt) }))
    .filter((row) => !query ||
      [row.date, row.campaign, row.progress, row.failed, row.skipped, row.status, row.lastError]
        .some((value) => value.toLowerCase().includes(query)),
    );
  return props.limit === undefined ? result : result.slice(0, props.limit);
});
</script>

<template>
  <Table
    :rows="rows"
    :row-key="(row) => row.key"
    :row-class="() => 'data-table__row--tasks'"
    header-class="data-table__row--tasks"
    :viewport-class="limit === undefined ? 'data-table__viewport--campaigns' : undefined"
    :row-action="(row) => row.taskID === null ? editCampaign(row.campaignID!) : openTaskReport(row.taskID)"
    :row-label="(row) => `${row.taskID === null ? 'Edit campaign' : 'View report'}: ${row.campaign}`"
    :is-row-active="(row) => row.taskID === selectedTaskID"
    empty-text="No campaigns found."
  >
    <template #actions><slot name="actions" /></template>
    <template #search>
      <ExpandableSearch v-model="search" label="Search campaigns" />
    </template>
    <template #header>
        <div class="data-table__cell">Date</div>
        <div class="data-table__cell">Campaign</div>
        <div class="data-table__cell data-table__cell--right">Progress</div>
        <div class="data-table__cell data-table__cell--right">Failed</div>
        <div class="data-table__cell data-table__cell--right">Skipped</div>
        <div class="data-table__cell">Status</div>
    </template>
    <template #row="{ row }">
        <div class="data-table__cell data-table__cell--truncate" data-label="Date">{{ row.date }}</div>
        <div class="data-table__cell data-table__cell--truncate" data-label="Campaign">{{ row.campaign }}</div>
        <div class="data-table__cell data-table__cell--right" data-label="Progress">{{ row.progress }}</div>
        <div class="data-table__cell data-table__cell--right" data-label="Failed">{{ row.failed }}</div>
        <div class="data-table__cell data-table__cell--right" data-label="Skipped">{{ row.skipped }}</div>
        <div class="data-table__cell data-table__cell--truncate" data-label="Status">{{ row.status }}</div>
    </template>
  </Table>
</template>
