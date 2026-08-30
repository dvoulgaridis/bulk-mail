<script setup lang="ts">
import { useWorkspace } from "../app/context";

const { notifications, dismissNotification } = useWorkspace();
</script>

<template>
  <TransitionGroup name="notification-stack" tag="div" class="notification-stack" aria-live="polite">
    <article v-for="item in notifications" :key="item.id" class="notification-panel" :data-type="item.type">
      <p>{{ item.text }}</p>
      <button type="button" aria-label="Dismiss notification" @click="dismissNotification(item.id)">&times;</button>
    </article>
  </TransitionGroup>
</template>
