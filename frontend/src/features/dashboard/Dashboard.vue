<script setup lang="ts">
import { computed } from "vue";
import { useWorkspace } from "../../app/context";
import CampaignTable from "../campaigns/CampaignTable.vue";

const { state, navigate } = useWorkspace();
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
    <div class="bulk-mail-section-head"><h3>Campaigns</h3></div>
    <CampaignTable :limit="5" />
  </section>
</template>
