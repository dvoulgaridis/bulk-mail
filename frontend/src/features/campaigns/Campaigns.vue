<script setup lang="ts">
import { computed } from "vue";
import { useWorkspace } from "../../app/context";
import PlusButton from "../../components/PlusButton.vue";
import TaskReport from "../reports/TaskReport.vue";
import { useReportsFeature } from "../reports/useReports";
import CampaignTable from "./CampaignTable.vue";
import { useCampaignsFeature } from "./useCampaigns";

const { state } = useWorkspace();
const { openNewCampaign, editCampaign } = useCampaignsFeature();
const {
  selectedTask,
  isSelectedTaskActive,
  viewSelectedTaskReport,
  cancelSelectedTask,
} = useReportsFeature();
const selectedCampaign = computed(() =>
  state.campaigns.find((campaign) => campaign.id === selectedTask.value?.campaignId),
);
</script>

<template>
  <section class="bulk-mail-section">
    <CampaignTable>
      <template #actions>
        <PlusButton label="New campaign" @click="openNewCampaign" />
      </template>
    </CampaignTable>
    <div v-if="selectedTask" class="app-stage-actions app-stage-actions--start">
      <span class="app-stage-note">Selected {{ selectedTask.campaignName || "Unnamed campaign" }}</span>
      <button v-if="selectedCampaign" type="button" @click="editCampaign(selectedCampaign.id)">Edit campaign</button>
      <button type="button" @click="viewSelectedTaskReport">View report</button>
      <button v-if="isSelectedTaskActive" type="button" class="is-danger" @click="cancelSelectedTask">Cancel task</button>
    </div>
    <TaskReport />
  </section>
</template>
