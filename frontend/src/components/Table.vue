<script setup lang="ts" generic="Row">
import { computed, ref, watch } from "vue";
import type { CSSProperties } from "vue";

const props = defineProps<{
  rows: Row[];
  rowKey: (row: Row) => string;
  selectable?: boolean;
  selectionDisabled?: boolean;
  isRowSelectable?: (row: Row) => boolean;
  rowClass?: (row: Row) => string;
  rowStyle?: CSSProperties;
  headerClass?: string;
  viewportClass?: string;
  emptyText: string;
  rowAction?: (row: Row) => void;
  rowLabel?: (row: Row) => string;
  isRowActive?: (row: Row) => boolean;
}>();
const selectedKeys = defineModel<string[]>("selectedKeys", { default: () => [] });
const anchor = ref<string | null>(null);
const selected = computed(() => new Set(selectedKeys.value));
const selectableKeys = computed(() => props.rows
  .filter((row) => !props.isRowSelectable || props.isRowSelectable(row))
  .map(props.rowKey));
const selectedCount = computed(() => selectableKeys.value.filter((key) => selected.value.has(key)).length);
const allSelected = computed(() => selectableKeys.value.length > 0 &&
  selectedCount.value === selectableKeys.value.length);

// Only order/eligibility changes invalidate the anchor, not refreshed row objects.
watch(() => JSON.stringify([props.selectable, props.rows.map(props.rowKey), selectableKeys.value]), () => {
  anchor.value = null;
}, { flush: "sync" });

function setSelection(keys: string[], checked: boolean): void {
  const next = new Set(selectedKeys.value);
  for (const key of keys) {
    if (checked) next.add(key);
    else next.delete(key);
  }
  selectedKeys.value = [...next];
}

function selectRow(row: Row, event: MouseEvent): void {
  if (props.selectionDisabled || !(event.target instanceof HTMLInputElement)) return;
  const key = props.rowKey(row);
  const keys = selectableKeys.value;
  const start = anchor.value === null ? -1 : keys.indexOf(anchor.value);
  const end = keys.indexOf(key);
  if (end < 0) return;
  const range = event.shiftKey && start >= 0
    ? keys.slice(Math.min(start, end), Math.max(start, end) + 1)
    : [key];
  setSelection(range, event.target.checked);
  anchor.value = key;
}

function selectAll(event: Event): void {
  if (props.selectionDisabled || !(event.target instanceof HTMLInputElement)) return;
  setSelection(selectableKeys.value, event.target.checked);
  anchor.value = null;
}
</script>

<template>
  <div class="data-table">
    <div v-if="$slots.actions || $slots.search" class="data-table__toolbar">
      <div class="data-table__toolbar-start"><slot name="actions" /></div>
      <slot name="search" />
    </div>
    <div class="data-table__viewport" :class="viewportClass">
      <div class="data-table__row data-table__row--header" :class="headerClass" :style="rowStyle" role="row">
        <div v-if="selectable" class="data-table__cell data-table__cell--select" data-label="Select">
          <input
            type="checkbox" aria-label="Select all displayed rows"
            :checked="allSelected"
            :indeterminate="selectedCount > 0 && !allSelected"
            :disabled="selectionDisabled || selectableKeys.length === 0"
            @change="selectAll"
          />
        </div>
        <slot name="header" />
      </div>
      <div v-if="rows.length === 0" class="data-table__empty">{{ emptyText }}</div>
      <component
        :is="rowAction && !selectable ? 'button' : 'div'"
        v-for="row in rows" :key="rowKey(row)"
        :type="rowAction && !selectable ? 'button' : undefined"
        class="data-table__row"
        :class="[rowClass?.(row), { 'data-table__row--link': rowAction && !selectable }]"
        :style="rowStyle" role="row"
        :data-active="isRowActive?.(row) || undefined"
        :aria-label="!selectable ? rowLabel?.(row) : undefined"
        @click="!selectable && rowAction?.(row)"
      >
        <div v-if="selectable" class="data-table__cell data-table__cell--select" data-label="Select">
          <input
            v-if="!isRowSelectable || isRowSelectable(row)"
            type="checkbox" :aria-label="`Select ${rowLabel?.(row) || rowKey(row)}`"
            :checked="selected.has(rowKey(row))" :disabled="selectionDisabled"
            @click.stop="selectRow(row, $event)"
          />
        </div>
        <slot name="row" :row="row" />
      </component>
    </div>
  </div>
</template>

<style scoped>
.data-table__cell--select input[type="checkbox"] {
  width: 1.15rem;
  height: 1.15rem;
}

/* Keep select-all available in the address table's mobile field header. */
.data-table__row--field-header > .data-table__cell--select {
  display: grid;
}
</style>
