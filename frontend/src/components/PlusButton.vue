<script setup lang="ts">
defineProps<{ label: string }>();
</script>

<template>
  <button type="button" class="header-add-button" :aria-label="label" :title="label">
    <span aria-hidden="true">+</span>
  </button>
</template>
