<script setup lang="ts">
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";
import { useAddressListsFeature } from "./useAddressLists";

const { listSearch, listRows, openNewAddressList, edit } = useAddressListsFeature();
</script>

<template>
  <section class="bulk-mail-section">
    <div class="data-table">
      <div class="data-table__toolbar">
        <div class="data-table__toolbar-start">
          <PlusButton label="Add address list" @click="openNewAddressList" />
        </div>
        <ExpandableSearch v-model="listSearch" label="Search address lists" />
      </div>
      <div class="data-table__viewport">
        <div class="data-table__row data-table__row--header data-table__row--lists" role="row">
          <div class="data-table__cell">Time</div>
          <div class="data-table__cell">Address list</div>
          <div class="data-table__cell data-table__cell--center">Addresses</div>
          <div class="data-table__cell">Notes</div>
        </div>
        <div v-if="listRows.length === 0" class="data-table__empty">No address lists yet.</div>
        <button
          v-for="row in listRows"
          v-else
          :key="row.id"
          type="button"
          class="data-table__row data-table__row--link data-table__row--lists"
          role="row"
          @click="edit(row.id)"
        >
          <div class="data-table__cell data-table__cell--truncate" data-label="Time">{{ row.time }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Address list">{{ row.name }}</div>
          <div class="data-table__cell data-table__cell--center" data-label="Addresses">{{ row.addresses }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Notes">{{ row.notes }}</div>
        </button>
      </div>
    </div>
  </section>
</template>
