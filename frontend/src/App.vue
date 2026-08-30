<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from "vue";
import { provideApplication } from "./app";
import NotificationStack from "./components/NotificationStack.vue";
import CampaignEditor from "./features/campaigns/CampaignEditor.vue";
import CampaignList from "./features/campaigns/CampaignList.vue";
import Dashboard from "./features/dashboard/Dashboard.vue";
import ProfileDetail from "./features/profiles/ProfileDetail.vue";
import ProfileList from "./features/profiles/ProfileList.vue";
import AddressList from "./features/address-lists/AddressList.vue";
import AddressLists from "./features/address-lists/AddressLists.vue";
import ColumnMapping from "./features/address-lists/ColumnMapping.vue";
import Settings from "./features/settings/Settings.vue";
import Suppressions from "./features/suppressions/Suppressions.vue";

const { workspace, profiles, settings } = provideApplication();
const {
  currentView,
  currentViewLabel,
  loading,
  ready,
  busy,
  state,
  bootstrap,
  disconnectTaskEvents,
  navigate,
} = workspace;

const theme = computed(() => state.settings?.theme);

watch(
  theme,
  (value) => {
    if (value) document.documentElement.dataset.theme = value;
    else delete document.documentElement.dataset.theme;
  },
  { immediate: true },
);

onMounted(() => {
  profiles.connectGoogleOAuthEvents();
  void bootstrap();
});
onBeforeUnmount(() => {
  profiles.disconnectGoogleOAuthEvents();
  disconnectTaskEvents();
});
</script>

<template>
  <div class="container">
    <div class="app-shell app-shell--bulk-mail app-accent-blue" :aria-busy="busy || undefined">
      <header class="app-header">
        <button
          v-if="currentView !== 'dashboard'"
          type="button"
          class="app-back"
          aria-label="Dashboard"
          @click="navigate('dashboard')"
        >
          <span aria-hidden="true">&larr;</span>
        </button>
        <span v-else class="app-back app-back--placeholder" aria-hidden="true"></span>

        <div class="app-header-identity">
          <strong>Bulk Mail</strong>
          <small>{{ currentViewLabel }}</small>
        </div>

        <div class="app-header-actions">
          <button
            type="button"
            class="app-theme-button theme-toggle"
            :disabled="!ready"
            :aria-label="theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
            @click="settings.setTheme(theme === 'dark' ? 'light' : 'dark')"
          >
            <svg
              class="theme-icon"
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              preserveAspectRatio="xMidYMid meet"
              style="
                shape-rendering: geometricPrecision;
                text-rendering: geometricPrecision;
                image-rendering: optimizeQuality;
              "
              aria-hidden="true"
            >
              <defs>
                <clipPath id="bulk-mail-theme-half-left">
                  <rect x="0" y="0" width="12" height="24" />
                </clipPath>
              </defs>
              <circle cx="12" cy="12" r="10.25" fill="none" stroke="currentColor" />
              <circle cx="12" cy="12" r="10.25" fill="currentColor" clip-path="url(#bulk-mail-theme-half-left)" />
            </svg>
          </button>
          <button
            type="button"
            class="app-theme-button"
            aria-label="Settings"
            :disabled="!ready"
            @click="settings.open"
          >
            <svg class="app-header-icon" aria-hidden="true" viewBox="0 0 24 24">
              <path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z" />
              <path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.88-.34A1.7 1.7 0 0 0 14 20.91V21a2 2 0 0 1-4 0v-.09a1.7 1.7 0 0 0-1.03-1.56 1.7 1.7 0 0 0-1.88.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.63 15 1.7 1.7 0 0 0 3.07 14H3a2 2 0 0 1 0-4h.09A1.7 1.7 0 0 0 4.65 9a1.7 1.7 0 0 0-.34-1.88l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.63 1.7 1.7 0 0 0 10 3.07V3a2 2 0 0 1 4 0v.09A1.7 1.7 0 0 0 15 4.65a1.7 1.7 0 0 0 1.88-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06-.06A1.7 1.7 0 0 0 19.37 9c.23.63.82 1 1.5 1H21a2 2 0 0 1 0 4h-.09A1.7 1.7 0 0 0 19.4 15Z" />
            </svg>
          </button>
        </div>
      </header>

      <div v-if="loading" class="app-stage-loading"><p>Preparing the Bulk Mail workspace...</p></div>

      <div v-else-if="!ready" class="app-stage-loading"><p>The Bulk Mail workspace could not be loaded.</p></div>

      <main v-else class="app-content" role="region" aria-label="Bulk Mail content">
        <div class="app-content-route">
          <Dashboard v-if="currentView === 'dashboard'" />
          <ProfileList v-else-if="currentView === 'profiles'" />
          <ProfileDetail v-else-if="currentView === 'profile-detail'" />
          <AddressLists v-else-if="currentView === 'address-lists'" />
          <AddressList v-else-if="currentView === 'address-list-detail'" />
          <ColumnMapping v-else-if="currentView === 'mapping'" />
          <CampaignList v-else-if="currentView === 'campaigns'" />
          <CampaignEditor v-else-if="currentView === 'new-campaign'" />
          <Suppressions v-else-if="currentView === 'suppressions'" />
          <Settings v-else-if="currentView === 'settings'" />
        </div>
      </main>

      <NotificationStack />
    </div>
  </div>
</template>
