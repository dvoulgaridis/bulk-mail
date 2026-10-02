import { computed, reactive, ref } from "vue";
import { api } from "../api/client";
import {
  emptyWorkspaceState,
  type AppState,
  type Suppression,
  type TaskStreamPayload,
} from "../api/types";
import { TaskList } from "../features/tasks/TaskList";
import type { Notification, RouteName, WorkspaceContext } from "./context";

const sectionLabels: Record<RouteName, string> = {
  dashboard: "Dashboard",
  profiles: "Profiles",
  "profile-detail": "Profiles",
  "address-lists": "Address lists",
  "address-list-detail": "Address lists",
  mapping: "Address lists",
  campaigns: "Campaigns",
  "new-campaign": "New campaign",
  suppressions: "Suppressions",
  settings: "Settings",
};

export function createWorkspace(): WorkspaceContext {
  const currentView = ref<RouteName>("dashboard");
  const loading = ref(true);
  const ready = ref(false);
  const busy = ref(false);
  const notifications = ref<Notification[]>([]);
  const state = reactive(emptyWorkspaceState());
  const tasks = new TaskList();
  let notificationID = 0;
  let taskEvents: EventSource | null = null;

  const currentViewLabel = computed(() => sectionLabels[currentView.value]);

  async function bootstrap(): Promise<void> {
    loading.value = true;
    ready.value = false;
    try {
      await api.bootstrap();
      await refresh();
      ready.value = true;
      connectTaskEvents();
    } catch (error) {
      notify(errorMessage(error), "error");
    } finally {
      loading.value = false;
    }
  }

  async function refresh(): Promise<void> {
    Object.assign(state, await api.request<AppState>("/api/state"));
  }

  async function requestSuppressions(): Promise<void> {
    state.suppressions = await api.request<Suppression[]>("/api/suppressions");
  }

  function connectTaskEvents(): void {
    disconnectTaskEvents();
    taskEvents = new EventSource("/api/events/tasks");
    taskEvents.addEventListener("tasks-snapshot", (event) => {
      tasks.replaceAll(parseTaskEvent(event).tasks);
    });
    taskEvents.addEventListener("tasks-updated", (event) => {
      tasks.upsert(parseTaskEvent(event).tasks);
    });
  }

  function disconnectTaskEvents(): void {
    taskEvents?.close();
    taskEvents = null;
  }

  function navigate(route: RouteName): void {
    if (!ready.value) return;
    currentView.value = route;
  }

  function notify(text: string, type: Notification["type"] = "success"): void {
    const id = ++notificationID;
    notifications.value.unshift({ id, text, type });
    window.setTimeout(() => dismissNotification(id), 9000);
  }

  function dismissNotification(id: number): void {
    notifications.value = notifications.value.filter((item) => item.id !== id);
  }

  async function runAction(action: () => Promise<void>): Promise<void> {
    if (!ready.value) return;
    busy.value = true;
    try {
      await action();
    } catch (error) {
      notify(errorMessage(error), "error");
    } finally {
      busy.value = false;
    }
  }

  return {
    api,
    currentView,
    currentViewLabel,
    loading,
    ready,
    busy,
    notifications,
    state,
    tasks,
    bootstrap,
    refresh,
    requestSuppressions,
    disconnectTaskEvents,
    navigate,
    notify,
    dismissNotification,
    runAction,
  };
}

function parseTaskEvent(event: Event): TaskStreamPayload {
  return JSON.parse((event as MessageEvent<string>).data) as TaskStreamPayload;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
