import { inject, provide, reactive, ref, watch, type InjectionKey } from "vue";
import type { AppSettings, SettingsDependencies, Theme } from "../../api/types";
import type { WorkspaceContext } from "../../app/context";

type SettingsDraft = Partial<AppSettings>;

export type SettingsFeature = ReturnType<typeof createSettingsFeature>;

const settingsKey: InjectionKey<SettingsFeature> = Symbol("settings");

export function provideSettingsFeature(workspace: WorkspaceContext): SettingsFeature {
  const feature = createSettingsFeature(workspace);
  provide(settingsKey, feature);
  return feature;
}

export function useSettingsFeature(): SettingsFeature {
  const feature = inject(settingsKey);
  if (!feature) throw new Error("Settings feature is unavailable.");
  return feature;
}

function createSettingsFeature(workspace: WorkspaceContext) {
  const draft = reactive<SettingsDraft>({});
  const dependencies = ref<SettingsDependencies | null>(null);
  const dependenciesLoading = ref(false);
  const dependencyError = ref("");
  const autoSaveState = ref<"idle" | "pending" | "saving" | "saved" | "error">("idle");
  const autoSaveError = ref("");
  let autoSaveTimer: number | undefined;
  let saveQueue: Promise<void> = Promise.resolve();
  let draftInitialized = false;

  watch(
    () => [
      draft.emailRatePerMin,
      draft.emailIntervalMs,
      draft.maxCampaignAddressEntries,
      draft.maxCampaignDocuments,
    ] as const,
    scheduleAutoSave,
  );

  function open(): void {
    const persisted = workspace.state.settings;
    if (!workspace.ready.value || !persisted) return;
    if (!draftInitialized) {
      Object.assign(draft, persisted);
      draftInitialized = true;
    }
    workspace.navigate("settings");
    void loadDependencies();
  }

  async function loadDependencies(): Promise<void> {
    dependenciesLoading.value = true;
    dependencies.value = null;
    dependencyError.value = "";
    try {
      dependencies.value = await workspace.api.request<SettingsDependencies>("/api/settings/dependencies");
    } catch (error) {
      dependencies.value = null;
      dependencyError.value = error instanceof Error ? error.message : String(error);
    } finally {
      dependenciesLoading.value = false;
    }
  }

  async function setTheme(theme: Theme): Promise<void> {
    const persisted = workspace.state.settings;
    if (!workspace.ready.value || !persisted) return;
    clearAutoSaveTimer();
    const previousTheme = persisted.theme;
    draft.theme = theme;
    persisted.theme = theme;
    await workspace.runAction(async () => {
      try {
        const editableSettings = workspace.currentView.value === "settings" ? draftSettings() : null;
        const saved = await enqueueSave({ ...(editableSettings || persisted), theme });
        applyPersisted(saved);
      } catch (error) {
        draft.theme = previousTheme;
        persisted.theme = previousTheme;
        throw error;
      }
    });
  }

  function scheduleAutoSave(): void {
    const settings = draftSettings();
    const persisted = workspace.state.settings;
    clearAutoSaveTimer();
    if (!settings || !persisted || settingsEqual(settings, persisted)) {
      autoSaveState.value = "idle";
      autoSaveError.value = "";
      return;
    }
    autoSaveState.value = "pending";
    autoSaveError.value = "";
    autoSaveTimer = window.setTimeout(() => {
      autoSaveTimer = undefined;
      void persistDraft(settings);
    }, 450);
  }

  async function persistDraft(settings: AppSettings): Promise<void> {
    autoSaveState.value = "saving";
    autoSaveError.value = "";
    try {
      const saved = await enqueueSave(settings);
      applyPersisted(saved);
      autoSaveState.value = "saved";
    } catch (error) {
      autoSaveState.value = "error";
      autoSaveError.value = error instanceof Error ? error.message : String(error);
    }
  }

  function draftSettings(): AppSettings | null {
    const theme = draft.theme;
    const emailRatePerMin = Number(draft.emailRatePerMin);
    const emailIntervalMs = Number(draft.emailIntervalMs);
    const maxCampaignAddressEntries = Number(draft.maxCampaignAddressEntries);
    const maxCampaignDocuments = Number(draft.maxCampaignDocuments);
    if (theme !== "light" && theme !== "dark") return null;
    if (!validInteger(emailRatePerMin, 1, 10000)) return null;
    if (!validInteger(emailIntervalMs, 100, 3600000)) return null;
    if (!validInteger(maxCampaignAddressEntries, 1, 10000)) return null;
    if (!validInteger(maxCampaignDocuments, 1, 5)) return null;
    return {
      theme,
      emailRatePerMin,
      emailIntervalMs,
      maxCampaignAddressEntries,
      maxCampaignDocuments,
    };
  }

  function clearAutoSaveTimer(): void {
    if (autoSaveTimer === undefined) return;
    window.clearTimeout(autoSaveTimer);
    autoSaveTimer = undefined;
  }

  function enqueueSave(settings: AppSettings): Promise<AppSettings> {
    const request = saveQueue.then(() => saveSettings(settings));
    saveQueue = request.then(
      () => undefined,
      () => undefined,
    );
    return request;
  }

  function saveSettings(settings: AppSettings): Promise<AppSettings> {
    return workspace.api.request<AppSettings>("/api/settings", { method: "PUT", body: settings });
  }

  function applyPersisted(saved: AppSettings): void {
    workspace.state.settings = saved;
  }

  return { draft, dependencies, dependenciesLoading, dependencyError, autoSaveState, autoSaveError, open, setTheme };
}

function validInteger(value: number, minimum: number, maximum: number): boolean {
  return Number.isInteger(value) && value >= minimum && value <= maximum;
}

function settingsEqual(left: AppSettings, right: AppSettings): boolean {
  return left.theme === right.theme
    && left.emailRatePerMin === right.emailRatePerMin
    && left.emailIntervalMs === right.emailIntervalMs
    && left.maxCampaignAddressEntries === right.maxCampaignAddressEntries
    && left.maxCampaignDocuments === right.maxCampaignDocuments;
}
