<script setup lang="ts">
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";
import Table from "../../components/Table.vue";
import { useAddressListsFeature } from "./useAddressLists";

const { listSearch, listRows, openNewAddressList, edit } = useAddressListsFeature();
</script>

<template>
  <section class="bulk-mail-section">
    <Table
      :rows="listRows"
      :row-key="(row) => String(row.id)"
      :row-class="() => 'data-table__row--lists'"
      header-class="data-table__row--lists"
      :row-action="(row) => edit(row.id)"
      empty-text="No address lists yet."
    >
      <template #actions>
        <PlusButton label="Add address list" @click="openNewAddressList" />
      </template>
      <template #search>
        <ExpandableSearch v-model="listSearch" label="Search address lists" />
      </template>
      <template #header>
        <div class="data-table__cell">Time</div>
        <div class="data-table__cell">Address list</div>
        <div class="data-table__cell data-table__cell--center">Addresses</div>
        <div class="data-table__cell">Notes</div>
      </template>
      <template #row="{ row }">
        <div class="data-table__cell data-table__cell--truncate" data-label="Time">{{ row.time }}</div>
        <div class="data-table__cell data-table__cell--truncate" data-label="Address list">{{ row.name }}</div>
        <div class="data-table__cell data-table__cell--center" data-label="Addresses">{{ row.addresses }}</div>
        <div class="data-table__cell data-table__cell--truncate" data-label="Notes">{{ row.notes }}</div>
      </template>
    </Table>
  </section>
</template>
