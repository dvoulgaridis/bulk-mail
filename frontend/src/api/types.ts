export type Theme = "light" | "dark";

export type AppSettings = {
  theme: Theme;
  emailRatePerMin: number;
  emailIntervalMs: number;
  maxCampaignAddressEntries: number;
  maxCampaignDocuments: number;
};

export type DependencyStatus = {
  available: boolean;
  path?: string;
  version?: string;
  error?: string;
};

export type SettingsDependencies = {
  libreOffice: DependencyStatus;
};

export type ProfileType = "smtp" | "gmail_app_password" | "gmail_oauth";

export type SMTPProfile = {
  id: number;
  name: string;
  profileType: ProfileType;
  host: string;
  port: number;
  tlsMode: string;
  username: string;
  senderEmail: string;
  senderName: string;
  replyTo: string;
  passwordExists: boolean;
  hasGoogleOAuth: boolean;
  createdAt: string;
  updatedAt: string;
};

export type SMTPProfileInput = Omit<
  SMTPProfile,
  "id" | "passwordExists" | "hasGoogleOAuth" | "createdAt" | "updatedAt"
> & {
  newPassword?: string;
};

export type SMTPEndpoint = {
  host: string;
  port: number;
  tlsMode: "starttls" | "tls";
};

export type SMTPDetectResponse = {
  endpoint: SMTPEndpoint | null;
};

export type AddressEntry = {
  id: number;
  email: string;
  displayName: string;
  fields: Record<string, string>;
};

export type AddressFieldRole = "" | "email" | "first_name" | "last_name";

export type AddressFieldDefinition = {
  key: string;
  label: string;
  role: AddressFieldRole;
  position: number;
};

export type AddressList = {
  id: number;
  name: string;
  source: string;
  notes: string;
  fields: AddressFieldDefinition[];
  entries?: AddressEntry[];
  count: number;
  createdAt: string;
  updatedAt: string;
};

export type Attachment = {
  filename: string;
  outputFilename: string;
  contentType?: string;
  size: number;
  contentBase64?: string;
};

export type MessageContent = {
  subject: string;
  body: string;
  htmlBody: string;
  requestDeliveryNotice: boolean;
  attachments: Attachment[];
};

export type PersonalizationOptions = {
  removeDiacritics: boolean;
  firstNameFormat: string;
  lastNameFormat: string;
  fullNameFormat: string;
};

export type Campaign = {
  id: number;
  name: string;
  addressListId: number;
  profileId: number | null;
  message: MessageContent;
  personalization: PersonalizationOptions;
  createdAt: string;
  updatedAt: string;
};

export type SaveCampaignCommand = Pick<
  Campaign,
  "name" | "addressListId" | "profileId" | "message" | "personalization"
>;

export type PreflightCampaignCommand = {
  mode: "send" | "generate";
  addressListId: number;
  message: MessageContent;
  personalization: PersonalizationOptions;
  sampleAddressEntryId: number;
};

export type ExecuteCampaignCommand = {
  campaignId: number;
  confirmedUnresolved: string[];
};

export type Task = {
  id: number;
  campaignId: number | null;
  campaignName: string;
  status: string;
  total: number;
  sent: number;
  failed: number;
  skipped: number;
  lastError: string;
  createdAt: string;
  updatedAt: string;
};

export type MessageDelivery = {
  id: number;
  taskId: number;
  campaignId: number | null;
  addressEntryId: number | null;
  email: string;
  status: string;
  attempt: number;
  providerMessageId: string;
  lastError: string;
  createdAt: string;
  updatedAt: string;
};

export type Suppression = {
  id: number;
  email: string;
  reason: string;
  createdAt: string;
};

export type AppLimits = {
  maxCampaignAttachmentBytes: number;
  maxAddressListFields: number;
};

export type AppIntegrations = {
  google: {
    oauthConfigured: boolean;
    sendEndpoint: string;
  };
};

export type AppState = {
  settings: AppSettings;
  addressFieldDefaults: AddressFieldDefinition[];
  smtpProfiles: SMTPProfile[];
  addressLists: AddressList[];
  campaigns: Campaign[];
  suppressions: Suppression[];
  limits: AppLimits;
  integrations: AppIntegrations;
};

export type WorkspaceState = Omit<AppState, "settings"> & {
  settings: AppSettings | null;
  tasks: Task[];
};

export type TaskStreamPayload = {
  tasks: Task[];
};

export type CampaignPreview = {
  count: number;
  previews: Array<{
    email: string;
    name: string;
    subject: string;
    body: string;
    htmlBody: string;
  }>;
  unresolved: string[];
};

export type PreflightIssue = {
  key: string;
  reason: "missing_field" | "never_populated";
  locations: string[];
};

export type PreflightAttachment = {
  filename: string;
  contentType: string;
  size: number;
  pageCount: number;
  contentBase64: string;
};

export type CampaignPreflight = {
  count: number;
  attachments: Array<{
    filename: string;
    placeholders: string[];
    convertedToPdf: boolean;
  }>;
  unresolved: PreflightIssue[];
  confirmation: string[];
  samples: Array<{
    addressEntryId: number;
    email: string;
    name: string;
    subject: string;
    body: string;
    htmlBody: string;
    attachments: PreflightAttachment[];
  }>;
  libreOfficeChecked: boolean;
};

export type TaskReport = {
  task: Task;
  metadata: {
    mode: "send" | "generate";
    campaignName: string;
    profileName?: string;
    listName: string;
    attachmentNames: string[];
  };
  deliveries: MessageDelivery[];
  statusCounts: Record<string, number>;
  failures: Array<{ category: string; count: number }>;
  archiveAvailable: boolean;
};

export type OAuthStart = {
  authUrl: string;
};

export type OAuthCompletion = {
  type: "google-oauth-complete" | "google-oauth-error";
  profileId: number;
  message: string;
};

export function emptyWorkspaceState(): WorkspaceState {
  return {
    settings: null,
    addressFieldDefaults: [],
    smtpProfiles: [],
    addressLists: [],
    campaigns: [],
    suppressions: [],
    limits: {
      maxCampaignAttachmentBytes: 0,
      maxAddressListFields: 0,
    },
    integrations: {
      google: {
        oauthConfigured: false,
        sendEndpoint: "",
      },
    },
    tasks: [],
  };
}
