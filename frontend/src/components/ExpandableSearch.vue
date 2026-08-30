<script setup lang="ts">
import { nextTick, ref } from "vue";

defineProps<{ label: string }>();

const query = defineModel<string>({ required: true });
const expanded = ref(query.value.length > 0);
const button = ref<HTMLButtonElement | null>(null);
const input = ref<HTMLInputElement | null>(null);

async function expand(): Promise<void> {
  expanded.value = true;
  await nextTick();
  input.value?.focus();
}

function collapseWhenEmpty(): void {
  if (query.value.length === 0) expanded.value = false;
}

async function clearAndCollapse(): Promise<void> {
  query.value = "";
  expanded.value = false;
  await nextTick();
  button.value?.focus();
}
</script>

<template>
  <div class="expandable-search" :data-expanded="expanded || undefined">
    <button ref="button" type="button" class="expandable-search__button" :aria-label="label" :aria-expanded="expanded" @click="expand">
      <svg aria-hidden="true" viewBox="0 0 24 24">
        <circle cx="11" cy="11" r="6.5" />
        <path d="m16 16 4 4" />
      </svg>
    </button>
    <input
      ref="input"
      v-model="query"
      type="search"
      placeholder="Search"
      :aria-label="label"
      :aria-hidden="!expanded"
      :tabindex="expanded ? 0 : -1"
      @blur="collapseWhenEmpty"
      @keydown.esc.prevent="clearAndCollapse"
    />
  </div>
</template>
