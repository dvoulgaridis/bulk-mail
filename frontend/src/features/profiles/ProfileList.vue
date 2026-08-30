<script setup lang="ts">
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";
import { useProfilesFeature } from "./useProfiles";

const { search, rows, openNewProfile, edit } = useProfilesFeature();
</script>

<template>
  <section class="bulk-mail-section">
    <div class="data-table">
      <div class="data-table__toolbar">
        <div class="data-table__toolbar-start">
          <PlusButton label="Add profile" @click="openNewProfile" />
        </div>
        <ExpandableSearch v-model="search" label="Search profiles" />
      </div>
      <div class="data-table__viewport">
        <div class="data-table__row data-table__row--header data-table__row--profiles" role="row">
          <div class="data-table__cell">Time</div>
          <div class="data-table__cell">Profile</div>
          <div class="data-table__cell">Sender</div>
          <div class="data-table__cell">Transport</div>
          <div class="data-table__cell">Connection</div>
        </div>
        <div v-if="rows.length === 0" class="data-table__empty">No profiles yet.</div>
        <button v-for="row in rows" v-else :key="row.id" type="button" class="data-table__row data-table__row--link data-table__row--profiles" role="row" @click="edit(row.id)">
          <div class="data-table__cell data-table__cell--truncate" data-label="Time">{{ row.time }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Profile">{{ row.name }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Sender">{{ row.sender }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Transport">{{ row.transport }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Connection">{{ row.smtp }}</div>
        </button>
      </div>
    </div>
  </section>
</template>
