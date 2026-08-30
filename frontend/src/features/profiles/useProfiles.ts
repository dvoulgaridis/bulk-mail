import { computed, inject, provide, reactive, ref, type InjectionKey } from "vue";
import type {
  OAuthCompletion,
  OAuthStart,
  ProfileType,
  SMTPDetectResponse,
  SMTPProfile,
  SMTPProfileInput,
} from "../../api/types";
import type { WorkspaceContext } from "../../app/context";
import { senderLabel, shortDate, smtpLabel, transportLabel } from "../../common/format";
import {
  CUSTOM_SMTP_PRESETS,
  GMAIL_APP_PASSWORD_ENDPOINT,
} from "./smtpPresets";

const PROFILE_TYPE_SMTP = "smtp";
const PROFILE_TYPE_GMAIL_APP_PASSWORD = "gmail_app_password";
const PROFILE_TYPE_GMAIL_OAUTH = "gmail_oauth";
const DEFAULT_SMTP_PRESET = "custom";
const PASSWORD_MASK = "••••••••••••";

type ProfileTypeChoice = "" | ProfileType;

type ProfileDraft = Omit<SMTPProfileInput, "newPassword" | "profileType"> & {
  id: number;
  profileType: ProfileTypeChoice;
  passwordExists: boolean;
  hasGoogleOAuth: boolean;
  newPassword: string;
  toEmail: string;
};

export type ProfilesFeature = ReturnType<typeof createProfilesFeature>;

const profilesKey: InjectionKey<ProfilesFeature> = Symbol("profiles");

export function provideProfilesFeature(workspace: WorkspaceContext): ProfilesFeature {
  const feature = createProfilesFeature(workspace);
  provide(profilesKey, feature);
  return feature;
}

export function useProfilesFeature(): ProfilesFeature {
  const feature = inject(profilesKey);
  if (!feature) throw new Error("Profiles feature is unavailable.");
  return feature;
}

function createProfilesFeature(workspace: WorkspaceContext) {
  const search = ref("");
  const draft = reactive<ProfileDraft>(emptyProfile());
  const profileTypeChoice = ref<ProfileTypeChoice>("");
  const savedProfileType = ref<ProfileTypeChoice>("");
  const smtpPresetID = ref("");
  const passwordFocused = ref(false);
  const smtpUsernameEdited = ref(false);
  let oauthChannel: BroadcastChannel | null = null;

  const rows = computed(() => {
    const query = search.value.trim().toLowerCase();
    return workspace.state.smtpProfiles
      .filter(
        (profile) =>
          !query ||
          [profile.name, senderLabel(profile), transportLabel(profile), smtpLabel(profile)].some(
            (value) => value.toLowerCase().includes(query),
          ),
      )
      .map((profile) => ({
        id: profile.id,
        time: shortDate(profile.createdAt),
        name: profile.name,
        sender: senderLabel(profile),
        transport: transportLabel(profile),
        smtp: smtpLabel(profile),
      }));
  });

  const isGmailOAuth = computed(() => draft.profileType === PROFILE_TYPE_GMAIL_OAUTH);
  const isGmailAppPassword = computed(
    () => draft.profileType === PROFILE_TYPE_GMAIL_APP_PASSWORD,
  );
  const hasSavedGoogleOAuth = computed(
    () => savedProfileType.value === PROFILE_TYPE_GMAIL_OAUTH && draft.hasGoogleOAuth,
  );
  const hasSavedGmailAppPassword = computed(
    () => savedProfileType.value === PROFILE_TYPE_GMAIL_APP_PASSWORD,
  );
  const hasSavedCurrentProfileType = computed(
    () => draft.id > 0 && savedProfileType.value === draft.profileType,
  );
  const passwordExists = computed(
    () => draft.passwordExists && hasSavedCurrentProfileType.value,
  );
  const passwordInputValue = computed({
    get: () =>
      passwordExists.value && !passwordFocused.value && draft.newPassword === ""
        ? PASSWORD_MASK
        : draft.newPassword,
    set: (value: string) => {
      draft.newPassword = value;
    },
  });

  function openNewProfile(): void {
    clearSensitive();
    Object.assign(draft, emptyProfile());
    profileTypeChoice.value = "";
    savedProfileType.value = "";
    smtpPresetID.value = "";
    workspace.navigate("profile-detail");
  }

  function edit(id: number): void {
    const profile = workspace.state.smtpProfiles.find((item) => item.id === id);
    if (!profile) return;
    clearSensitive();
    applyProfile(profile);
    workspace.navigate("profile-detail");
  }

  function selectProfileType(value: ProfileTypeChoice): void {
    if (profileTypeChoice.value === value) return;
    clearSensitive();
    clearConnection();
    profileTypeChoice.value = value;
    draft.profileType = value;
    if (value === PROFILE_TYPE_SMTP) {
      smtpUsernameEdited.value = false;
      applySMTPPreset(DEFAULT_SMTP_PRESET);
      syncSMTPUsername();
      return;
    }
    if (value === PROFILE_TYPE_GMAIL_APP_PASSWORD) {
      draft.name ||= "Gmail";
      Object.assign(draft, GMAIL_APP_PASSWORD_ENDPOINT);
      syncSMTPUsername();
    }
  }

  function changeProfileType(event: Event): void {
    const target = event.target;
    if (target instanceof HTMLSelectElement) {
      selectProfileType(target.value as ProfileTypeChoice);
    }
  }

  function applySMTPPreset(presetID: string): void {
    if (draft.profileType !== PROFILE_TYPE_SMTP) return;
    const preset = CUSTOM_SMTP_PRESETS.find((item) => item.id === presetID);
    if (!preset) return;
    smtpPresetID.value = preset.id;
    draft.host = preset.host;
    draft.port = preset.port;
    draft.tlsMode = preset.tlsMode;
  }

  function normalizeConnection(): void {
    if (Number(draft.port) === 465) draft.tlsMode = "tls";
  }

  function syncSMTPUsername(): void {
    if (!smtpUsernameEdited.value) draft.username = draft.senderEmail;
  }

  function markSMTPUsernameEdited(): void {
    smtpUsernameEdited.value = true;
  }

  async function requestDetectSMTP(): Promise<void> {
    if (draft.profileType !== PROFILE_TYPE_SMTP) return;
    await workspace.runAction(async () => {
      const result = await workspace.api.request<SMTPDetectResponse>("/api/smtp/detect", {
        method: "POST",
        body: { email: draft.senderEmail },
      });
      if (!result.endpoint) {
        workspace.notify("SMTP settings could not be detected. Enter them manually.", "error");
        return;
      }
      Object.assign(draft, result.endpoint);
      workspace.notify("SMTP settings detected.");
    });
  }

  async function save(): Promise<void> {
    const profileType = draft.profileType;
    if (!profileType) {
      workspace.notify("Choose a profile type.", "error");
      return;
    }
    if (isGmailOAuth.value && !draft.id) {
      workspace.notify("Connect with Google before saving the profile.", "error");
      return;
    }
    await workspace.runAction(async () => {
      if (isGmailAppPassword.value && !smtpUsernameEdited.value) {
        draft.username = draft.senderEmail;
      }
      normalizeConnection();
      const input: SMTPProfileInput = {
        name: draft.name,
        profileType,
        host: draft.host,
        port: Number(draft.port),
        tlsMode: draft.tlsMode,
        username: draft.username,
        senderEmail: draft.senderEmail,
        senderName: draft.senderName,
        replyTo: draft.replyTo,
        ...(draft.newPassword !== "" ? { newPassword: draft.newPassword } : {}),
      };
      const endpoint = draft.id ? `/api/smtp/profiles/${draft.id}` : "/api/smtp/profiles";
      const saved = await workspace.api.request<SMTPProfile>(endpoint, {
        method: draft.id ? "PUT" : "POST",
        body: input,
      });
      const gmailAppPassword = saved.profileType === PROFILE_TYPE_GMAIL_APP_PASSWORD;
      await workspace.refresh();
      const refreshed =
        workspace.state.smtpProfiles.find((profile) => profile.id === saved.id) || saved;
      applyProfile(refreshed);
      workspace.notify(
        gmailAppPassword
          ? "Gmail connection verified and profile saved."
          : "Profile saved.",
      );
    });
  }

  async function remove(): Promise<void> {
    if (!draft.id || !window.confirm("Delete this profile?")) return;
    await workspace.runAction(async () => {
      await workspace.api.request<void>(`/api/smtp/profiles/${draft.id}`, {
        method: "DELETE",
      });
      clearSensitive();
      await workspace.refresh();
      workspace.navigate("profiles");
      workspace.notify("Profile deleted.");
    });
  }

  async function test(sendEmail = false): Promise<void> {
    if (!draft.id) {
      workspace.notify("Save the profile before testing it.", "error");
      return;
    }
    await workspace.runAction(async () => {
      await workspace.api.request("/api/smtp/test", {
        method: "POST",
        body: { profileId: draft.id, toEmail: sendEmail ? draft.toEmail : "" },
      });
      workspace.notify(sendEmail ? "Test email sent." : "SMTP connection succeeded.");
    });
  }

  async function startGoogleOAuth(): Promise<void> {
    if (!draft.name.trim()) {
      workspace.notify("Enter a profile name before connecting with Google.", "error");
      return;
    }
    const popup = window.open("", "bulk-mail-google-oauth", "popup,width=520,height=720");
    if (!popup) {
      workspace.notify("Allow popups to connect with Google.", "error");
      return;
    }
    await workspace.runAction(async () => {
      try {
        const result = await workspace.api.request<OAuthStart>("/api/oauth/google/start", {
          method: "POST",
          body: { profileId: draft.id, profileName: draft.name },
        });
        popup.location.replace(result.authUrl);
      } catch (error) {
        popup.close();
        throw error;
      }
    });
  }

  function connectGoogleOAuthEvents(): void {
    disconnectGoogleOAuthEvents();
    oauthChannel = new BroadcastChannel("bulk-mail-google-oauth");
    oauthChannel.addEventListener("message", handleGoogleOAuthEvent);
  }

  function disconnectGoogleOAuthEvents(): void {
    oauthChannel?.removeEventListener("message", handleGoogleOAuthEvent);
    oauthChannel?.close();
    oauthChannel = null;
  }

  function clearSensitive(): void {
    draft.newPassword = "";
    passwordFocused.value = false;
    smtpUsernameEdited.value = false;
  }

  async function handleGoogleOAuthEvent(event: MessageEvent<OAuthCompletion>): Promise<void> {
    const result = event.data;
    if (
      !result ||
      (result.type !== "google-oauth-complete" && result.type !== "google-oauth-error")
    ) {
      return;
    }
    if (result.type === "google-oauth-error") {
      workspace.notify(result.message || "Google connection failed.", "error");
      return;
    }
    await workspace.runAction(async () => {
      await workspace.refresh();
      const profile = workspace.state.smtpProfiles.find(
        (item) => item.id === result.profileId,
      );
      if (!profile) throw new Error("Connected Gmail profile was not found.");
      if (workspace.currentView.value === "profile-detail") applyProfile(profile);
      workspace.notify(result.message || "Gmail connected successfully.");
    });
  }

  function applyProfile(profile: SMTPProfile): void {
    Object.assign(draft, {
      ...profile,
      newPassword: "",
      toEmail: draft.toEmail,
    });
    passwordFocused.value = false;
    profileTypeChoice.value = profile.profileType;
    savedProfileType.value = profile.profileType;
    smtpPresetID.value = "";
    smtpUsernameEdited.value = profile.username !== profile.senderEmail;
    normalizeConnection();
  }

  function clearConnection(): void {
    smtpPresetID.value = "";
    draft.host = "";
    draft.port = 0;
    draft.tlsMode = "starttls";
    draft.username = "";
  }

  function setPasswordFocused(value: boolean): void {
    passwordFocused.value = value;
  }

  return {
    customSMTPPresets: CUSTOM_SMTP_PRESETS,
    draft,
    profileTypeChoice,
    smtpPresetID,
    isGmailOAuth,
    hasSavedGoogleOAuth,
    hasSavedGmailAppPassword,
    hasSavedCurrentProfileType,
    passwordExists,
    passwordInputValue,
    search,
    rows,
    openNewProfile,
    edit,
    selectProfileType,
    changeProfileType,
    applySMTPPreset,
    normalizeConnection,
    syncSMTPUsername,
    markSMTPUsernameEdited,
    requestDetectSMTP,
    save,
    remove,
    test,
    startGoogleOAuth,
    connectGoogleOAuthEvents,
    disconnectGoogleOAuthEvents,
    clearSensitive,
    setPasswordFocused,
  };
}

function emptyProfile(): ProfileDraft {
  return {
    id: 0,
    profileType: "",
    name: "",
    host: "",
    port: 0,
    tlsMode: "starttls",
    username: "",
    passwordExists: false,
    hasGoogleOAuth: false,
    newPassword: "",
    senderEmail: "",
    senderName: "",
    replyTo: "",
    toEmail: "",
  };
}
