import { computed, inject, provide, ref, watch, type InjectionKey } from "vue";
import type { TaskReport } from "../../api/types";
import type { WorkspaceContext } from "../../app/context";
import { saveTextFile } from "../../common/files";
import { shortDate } from "../../common/format";

export type ReportsFeature = ReturnType<typeof createReportsFeature>;

const reportsKey: InjectionKey<ReportsFeature> = Symbol("reports");

export function provideReportsFeature(workspace: WorkspaceContext): ReportsFeature {
  const feature = createReportsFeature(workspace);
  provide(reportsKey, feature);
  return feature;
}

export function useReportsFeature(): ReportsFeature {
  const feature = inject(reportsKey);
  if (!feature) throw new Error("Reports feature is unavailable.");
  return feature;
}

function createReportsFeature(workspace: WorkspaceContext) {
  const search = ref("");
  const selectedTaskID = ref(0);
  const reportVisible = ref(false);
  const selectedTaskReport = ref<TaskReport | null>(null);
  const terminalStatuses = new Set(["completed", "completed_with_errors", "cancelled", "interrupted"]);
  let reportRequestID = 0;

  const taskRows = computed(() =>
    workspace.state.tasks.map((task) => ({
      id: task.id,
      date: shortDate(task.createdAt),
      campaign: task.campaignName || "Unnamed campaign",
      progress: `${Math.min(task.sent + task.failed + task.skipped, task.total)}/${task.total}`,
      failed: String(task.failed),
      skipped: String(task.skipped || 0),
      status: task.status,
      lastError: task.lastError || "",
    })),
  );
  const filteredTaskRows = computed(() => {
    const query = search.value.trim().toLowerCase();
    if (!query) return taskRows.value;
    return taskRows.value.filter((row) =>
      [row.date, row.campaign, row.progress, row.failed, row.skipped, row.status, row.lastError].some((value) =>
        String(value).toLowerCase().includes(query),
      ),
    );
  });
  const selectedTaskRow = computed(() => taskRows.value.find((row) => row.id === selectedTaskID.value) || null);
  const selectedTask = computed(() => workspace.state.tasks.find((item) => item.id === selectedTaskID.value) || null);
  const selectedTaskActive = computed(() =>
    ["queued", "preparing", "running"].includes(selectedTask.value?.status || ""),
  );

  watch(
    () => selectedTask.value,
    (task, previous) => {
      if (!task || !reportVisible.value || selectedTaskReport.value?.task.id !== task.id) return;
      selectedTaskReport.value = { ...selectedTaskReport.value, task };
      if (previous && !terminalStatuses.has(previous.status) && terminalStatuses.has(task.status)) {
        void workspace.runAction(loadSelectedTaskReport);
      }
    },
  );

  function selectTask(id: number): void {
    selectedTaskID.value = id;
    reportVisible.value = false;
    selectedTaskReport.value = null;
  }

  async function viewSelectedTaskReport(): Promise<void> {
    if (!selectedTaskID.value) return;
    reportVisible.value = true;
    await workspace.runAction(loadSelectedTaskReport);
  }

  async function openTaskReport(id: number): Promise<void> {
    selectTask(id);
    workspace.navigate("campaigns");
    await viewSelectedTaskReport();
  }

  async function loadSelectedTaskReport(): Promise<void> {
    if (!selectedTaskID.value) return;
    const taskID = selectedTaskID.value;
    const requestID = ++reportRequestID;
    let report = await workspace.api.request<TaskReport>(`/api/tasks/${taskID}`);
    let current = workspace.state.tasks.find((task) => task.id === taskID);
    if (current && terminalStatuses.has(current.status) && !terminalStatuses.has(report.task.status)) {
      report = await workspace.api.request<TaskReport>(`/api/tasks/${taskID}`);
      current = workspace.state.tasks.find((task) => task.id === taskID);
    }
    if (requestID !== reportRequestID || selectedTaskID.value !== taskID) return;
    selectedTaskReport.value = current ? { ...report, task: current } : report;
  }

  async function cancelSelectedTask(): Promise<void> {
    if (!selectedTaskID.value || !window.confirm("Cancel this task?")) return;
    await workspace.runAction(async () => {
      await workspace.api.request(`/api/tasks/${selectedTaskID.value}/cancel`, { method: "POST" });
      workspace.notify("Cancellation requested.");
    });
  }

  async function downloadArchive(): Promise<void> {
    const report = selectedTaskReport.value;
    if (!report) return;
    await workspace.runAction(async () => {
      await workspace.api.downloadGet(
        `/api/tasks/${report.task.id}/archive`,
        `${report.metadata.campaignName || "bulk-mail-documents"}.zip`,
      );
      await loadSelectedTaskReport();
    });
  }

  function exportResults(onlyUnsent = false): void {
    const report = selectedTaskReport.value;
    if (!report) return;
    const deliveries = onlyUnsent
      ? report.deliveries.filter((delivery) => !["sent", "generated"].includes(delivery.status))
      : report.deliveries;
    const rows = [
      ["email", "status", "attempts", "provider_message_id", "diagnostic"],
      ...deliveries.map((delivery) => [
        delivery.email, delivery.status, String(delivery.attempt), delivery.providerMessageId, delivery.lastError,
      ]),
    ];
    const suffix = onlyUnsent ? "failed-unsent" : "results";
    saveTextFile(
      `${safeFilePart(report.metadata.campaignName)}-${suffix}.csv`,
      rows.map(csvRow).join("\r\n") + "\r\n",
      "text/csv;charset=utf-8",
    );
  }

  async function suppressEmailAddress(email: string): Promise<void> {
    await workspace.runAction(async () => {
      await workspace.api.request("/api/suppressions", {
        method: "POST",
        body: { emails: [email], reason: "delivery report" },
      });
      await workspace.refresh();
      workspace.notify(`${email} added to suppressions.`);
    });
  }

  return {
    search,
    selectedTaskID,
    reportVisible,
    taskRows,
    filteredTaskRows,
    selectedTaskRow,
    selectedTaskReport,
    selectedTaskActive,
    selectTask,
    viewSelectedTaskReport,
    openTaskReport,
    loadSelectedTaskReport,
    cancelSelectedTask,
    downloadArchive,
    exportResults,
    suppressEmailAddress,
  };
}

function csvRow(values: string[]): string {
  return values.map((value) => `"${String(value || "").replaceAll('"', '""')}"`).join(",");
}

function safeFilePart(value: string): string {
  return (value || "campaign").trim().replace(/[<>:"/\\|?*\x00-\x1f]+/g, "_") || "campaign";
}
