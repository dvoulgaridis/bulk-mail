<script setup lang="ts">
import { computed, ref } from "vue";
import { useWorkspace } from "../../app/context";
import { saveTextFile } from "../../common/files";
import ExpandableSearch from "../../components/ExpandableSearch.vue";
import PlusButton from "../../components/PlusButton.vue";

const workspace = useWorkspace();
const email = ref("");
const reason = ref("manual");
const search = ref("");
const importInput = ref<HTMLInputElement | null>(null);
const rows = computed(() => {
  const query = search.value.trim().toLowerCase();
  return workspace.state.suppressions.filter((item) => !query || [item.email, item.reason].some((value) => value.toLowerCase().includes(query)));
});

async function add(): Promise<void> {
  const value = email.value.trim();
  if (!value) return;
  await workspace.runAction(async () => {
    await workspace.api.request("/api/suppressions", { method: "POST", body: { emails: [value], reason: reason.value } });
    email.value = "";
    await workspace.refresh();
  });
}

async function remove(id: number): Promise<void> {
  await workspace.runAction(async () => {
    await workspace.api.request(`/api/suppressions/${id}`, { method: "DELETE" });
    await workspace.refresh();
  });
}

function openImport(): void {
  importInput.value?.click();
}

async function importFile(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  const values = (await file.text()).split(/[\s,;]+/).map((value) => value.replace(/^"|"$/g, "").trim()).filter((value) => value.includes("@"));
  if (values.length === 0) {
    workspace.notify("The file does not contain any email addresses.", "error");
    return;
  }
  await workspace.runAction(async () => {
    await workspace.api.request("/api/suppressions", { method: "POST", body: { emails: values, reason: "import" } });
    await workspace.refresh();
    workspace.notify(`${values.length} suppression address${values.length === 1 ? "" : "es"} imported.`);
  });
}

function exportFile(): void {
  const content = ["email,reason", ...workspace.state.suppressions.map((item) => `${csv(item.email)},${csv(item.reason)}`)].join("\r\n") + "\r\n";
  saveTextFile("bulk-mail-suppressions.csv", content, "text/csv;charset=utf-8");
}

function csv(value: string): string {
  return `"${value.replaceAll('"', '""')}"`;
}
</script>

<template>
  <section class="bulk-mail-section">
    <form class="app-form" @submit.prevent="add">
      <div class="app-form-two-up">
        <label class="app-form-field"><span>Email</span><input v-model="email" type="email" placeholder="name@example.com" required /></label>
        <label class="app-form-field"><span>Reason</span><input v-model="reason" type="text" placeholder="manual" /></label>
      </div>
      <div class="app-stage-actions app-stage-actions--start"><PlusButton label="Add suppression" @click="add" /></div>
    </form>

    <div class="data-table">
      <div class="data-table__toolbar">
        <div class="data-table__toolbar-start">
          <input ref="importInput" class="visually-hidden" type="file" accept=".csv,.txt" @change="importFile" />
          <button type="button" class="data-table__action" @click="openImport">Import</button>
          <button type="button" class="data-table__action" :disabled="workspace.state.suppressions.length === 0" @click="exportFile">Export</button>
        </div>
        <ExpandableSearch v-model="search" label="Search suppressions" />
      </div>
      <div class="data-table__viewport">
        <div class="data-table__row data-table__row--header data-table__row--suppressions" role="row">
          <div class="data-table__cell">Email</div><div class="data-table__cell">Reason</div><div class="data-table__cell"></div>
        </div>
        <div v-if="rows.length === 0" class="data-table__empty">No suppressed addresses.</div>
        <div v-for="item in rows" v-else :key="item.id" class="data-table__row data-table__row--suppressions" role="row">
          <div class="data-table__cell data-table__cell--truncate" data-label="Email">{{ item.email }}</div>
          <div class="data-table__cell data-table__cell--truncate" data-label="Reason">{{ item.reason }}</div>
          <div class="data-table__cell" data-label="Action"><button type="button" class="data-table__action data-table__action--danger" @click="remove(item.id)">Remove</button></div>
        </div>
      </div>
    </div>
  </section>
</template>
