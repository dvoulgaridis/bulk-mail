import {
  computed,
  inject,
  provide,
  reactive,
  ref,
  useTemplateRef,
  watch,
  type InjectionKey,
} from "vue";
import type {
  AddressEntry,
  AddressList,
  Attachment,
  Campaign,
  CampaignPreflight,
  ExecuteCampaignCommand,
  PreflightCampaignCommand,
  SaveCampaignCommand,
  Task,
} from "../../api/types";
import type { WorkspaceContext } from "../../app/context";
import { fileToBase64, formatFileSize } from "../../common/files";
import { transportLabel } from "../../common/format";

type CampaignMode = "send" | "generate";

export type CampaignsFeature = ReturnType<typeof createCampaignsFeature>;

const campaignsKey: InjectionKey<CampaignsFeature> = Symbol("campaigns");

export function provideCampaignsFeature(
  workspace: WorkspaceContext,
  onTaskQueued: (taskID: number) => void,
): CampaignsFeature {
  const feature = createCampaignsFeature(workspace, onTaskQueued);
  provide(campaignsKey, feature);
  return feature;
}

export function useCampaignsFeature(): CampaignsFeature {
  const feature = inject(campaignsKey);
  if (!feature) throw new Error("Campaigns feature is unavailable.");
  return feature;
}

function createCampaignsFeature(
  workspace: WorkspaceContext,
  onTaskQueued: (taskID: number) => void,
) {
  const campaign = reactive<Campaign>(newCampaign());
  const mode = ref<CampaignMode>("send");
  const sampleAddressEntryID = ref(0);
  const fileInput = useTemplateRef<HTMLInputElement>("fileInput");
  const preflightResult = ref<CampaignPreflight | null>(null);
  const preflightSignature = ref("");
  const unresolvedConfirmed = ref(false);
  const sampleAddressEntries = ref<AddressEntry[]>([]);
  let savedSignature = "";

  const preflightCurrent = computed(
    () => preflightSignature.value !== "" && preflightSignature.value === requestSignature(),
  );
  const canRun = computed(
    () =>
      preflightCurrent.value &&
      !!preflightResult.value &&
      (preflightResult.value.confirmation.length === 0 || unresolvedConfirmed.value),
  );

  watch(
    () => campaign.addressListId,
    (listID) => {
      void loadSampleAddressEntries(listID);
    },
  );

  function openNewCampaign(): void {
    Object.assign(campaign, newCampaign(), {
      addressListId: workspace.state.addressLists[0]?.id || 0,
      profileId: workspace.state.smtpProfiles[0]?.id ?? null,
    });
    mode.value = "send";
    savedSignature = "";
    clearPreflight();
    workspace.navigate("new-campaign");
  }

  async function editCampaign(id: number): Promise<void> {
    if (id <= 0) return;
    await workspace.runAction(async () => {
      const loaded = await workspace.api.request<Campaign>(`/api/campaigns/${id}`);
      Object.assign(campaign, loaded);
      mode.value = "send";
      savedSignature = campaignSignature();
      clearPreflight();
      workspace.navigate("new-campaign");
    });
  }

  async function loadSampleAddressEntries(listID: number): Promise<void> {
    sampleAddressEntries.value = [];
    sampleAddressEntryID.value = 0;
    if (!listID) return;
    try {
      const addressList = await workspace.api.request<AddressList>(`/api/address-lists/${listID}`);
      if (campaign.addressListId === listID) sampleAddressEntries.value = addressList.entries || [];
    } catch (error) {
      workspace.notify(error instanceof Error ? error.message : String(error), "error");
    }
  }

  async function saveCampaign(): Promise<void> {
    await workspace.runAction(async () => {
      await ensureCampaignSaved();
      workspace.notify("Campaign saved.");
    });
  }

  async function deleteCampaign(): Promise<void> {
    if (campaign.id <= 0 || !window.confirm("Delete this saved campaign?")) return;
    await workspace.runAction(async () => {
      await workspace.api.request<void>(`/api/campaigns/${campaign.id}`, { method: "DELETE" });
      Object.assign(campaign, newCampaign());
      savedSignature = "";
      clearPreflight();
      await workspace.refresh();
      workspace.navigate("campaigns");
      workspace.notify("Campaign deleted.");
    });
  }

  async function ensureCampaignSaved(): Promise<number> {
    if (campaign.id > 0 && savedSignature === campaignSignature()) return campaign.id;
    const creating = campaign.id === -1;
    const saved = await workspace.api.request<Campaign>(
      creating ? "/api/campaigns" : `/api/campaigns/${campaign.id}`,
      { method: creating ? "POST" : "PUT", body: saveCommand() },
    );
    Object.assign(campaign, saved);
    savedSignature = campaignSignature();
    await workspace.refresh();
    return saved.id;
  }

  async function runPreflight(): Promise<void> {
    await workspace.runAction(async () => {
      const signature = requestSignature();
      const result = await workspace.api.request<CampaignPreflight>("/api/campaigns/preflight", {
        method: "POST",
        body: preflightCommand(),
      });
      preflightResult.value = result;
      preflightSignature.value = signature;
      unresolvedConfirmed.value = false;
    });
  }

  function openAttachmentPicker(): void {
    fileInput.value?.click();
  }

  async function handleAttachmentChange(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const files = Array.from(input.files || []);
    input.value = "";
    if (files.length === 0) return;
    const settings = workspace.state.settings;
    if (!settings) return;
    if (files.length > settings.maxCampaignDocuments) {
      workspace.notify(
        `Choose up to ${settings.maxCampaignDocuments} attachments.`,
        "error",
      );
      return;
    }
    const attachments: Attachment[] = [];
    for (const file of files) {
      if (file.size > workspace.state.limits.maxCampaignAttachmentBytes) {
        workspace.notify(
          `${file.name} is larger than ${formatFileSize(
            workspace.state.limits.maxCampaignAttachmentBytes,
          )}.`,
          "error",
        );
        return;
      }
      attachments.push({
        filename: file.name,
        outputFilename: isDOCXFilename(file.name) ? file.name.replace(/\.docx$/i, ".pdf") : "",
        size: file.size,
        contentBase64: await fileToBase64(file),
      });
    }
    campaign.message.attachments = attachments;
    clearPreflight();
  }

  function removeAttachment(index: number): void {
    campaign.message.attachments.splice(index, 1);
    clearPreflight();
  }

  function run(): void {
    if (!preflightCurrent.value || !preflightResult.value) {
      workspace.notify("Run preflight after the last campaign change.", "error");
      return;
    }
    if (preflightResult.value.confirmation.length > 0 && !unresolvedConfirmed.value) {
      workspace.notify("Confirm the exact unresolved placeholders before continuing.", "error");
      return;
    }
    if (mode.value === "generate") void generateDocuments();
    else void queueCampaign();
  }

  async function queueCampaign(): Promise<void> {
    await workspace.runAction(async () => {
      const campaignID = await ensureCampaignSaved();
      const task = await workspace.api.request<Task>("/api/campaigns/send", {
        method: "POST",
        body: executionCommand(campaignID),
      });
      await applyQueuedTask(task, "Campaign queued.");
    });
  }

  async function generateDocuments(): Promise<void> {
    if (!campaign.message.attachments.some(isDOCXAttachment)) {
      workspace.notify("Choose at least one DOCX document.", "error");
      return;
    }
    await workspace.runAction(async () => {
      const campaignID = await ensureCampaignSaved();
      const task = await workspace.api.request<Task>("/api/campaigns/generate", {
        method: "POST",
        body: executionCommand(campaignID),
      });
      await applyQueuedTask(task, "Document generation queued.");
    });
  }

  async function applyQueuedTask(task: Task, message: string): Promise<void> {
    workspace.mergeTasks([task]);
    await workspace.refresh();
    onTaskQueued(task.id);
    workspace.navigate("campaigns");
    workspace.notify(message);
  }

  function saveCommand(): SaveCampaignCommand {
    return {
      name: campaign.name,
      addressListId: campaign.addressListId,
      profileId: campaign.profileId,
      message: campaign.message,
      personalization: campaign.personalization,
    };
  }

  function preflightCommand(): PreflightCampaignCommand {
    return {
      mode: mode.value,
      addressListId: Number(campaign.addressListId),
      message: {
        ...campaign.message,
        attachments: attachmentPayload(),
      },
      personalization: campaign.personalization,
      sampleAddressEntryId: Number(sampleAddressEntryID.value),
    };
  }

  function executionCommand(campaignID: number): ExecuteCampaignCommand {
    return {
      campaignId: campaignID,
      confirmedUnresolved: unresolvedConfirmed.value
        ? preflightResult.value?.confirmation || []
        : [],
    };
  }

  function attachmentPayload(): Attachment[] {
    return campaign.message.attachments.map((attachment) => ({
      ...attachment,
      contentBase64: attachment.contentBase64 || "",
    }));
  }

  function requestSignature(): string {
    return JSON.stringify(preflightCommand());
  }

  function campaignSignature(): string {
    return JSON.stringify(campaign);
  }

  function clearPreflight(): void {
    preflightResult.value = null;
    preflightSignature.value = "";
    unresolvedConfirmed.value = false;
  }

  function viewSampleAttachment(
    attachment: CampaignPreflight["samples"][number]["attachments"][number],
  ): void {
    const binary = atob(attachment.contentBase64);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) {
      bytes[index] = binary.charCodeAt(index);
    }
    const url = URL.createObjectURL(new Blob([bytes], { type: attachment.contentType }));
    window.open(url, "_blank", "noopener,noreferrer");
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
  }

  return {
    campaign,
    mode,
    sampleAddressEntryID,
    preflightResult,
    preflightCurrent,
    unresolvedConfirmed,
    sampleAddressEntries,
    canRun,
    openNewCampaign,
    editCampaign,
    saveCampaign,
    deleteCampaign,
    runPreflight,
    openAttachmentPicker,
    handleAttachmentChange,
    removeAttachment,
    run,
    viewSampleAttachment,
    isDOCXAttachment,
    transportLabel,
    formatFileSize,
  };
}

function isDOCXAttachment(attachment: { filename: string }): boolean {
  return isDOCXFilename(attachment.filename);
}

function isDOCXFilename(filename: string): boolean {
  return filename.toLowerCase().endsWith(".docx");
}

function newCampaign(): Campaign {
  return {
    id: -1,
    name: "",
    addressListId: 0,
    profileId: null,
    message: {
      subject: "",
      body: "",
      htmlBody: "",
      requestDeliveryNotice: false,
      attachments: [],
    },
    personalization: {
      removeDiacritics: false,
      firstNameFormat: "preserve",
      lastNameFormat: "preserve",
      fullNameFormat: "preserve",
    },
    createdAt: "",
    updatedAt: "",
  };
}
