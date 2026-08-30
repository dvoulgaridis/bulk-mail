export type SMTPPreset = Readonly<{
  id: string;
  displayName: string;
  host: string;
  port: number;
  tlsMode: "starttls" | "tls";
}>;

export const CUSTOM_SMTP_PRESETS = [
  {
    id: "custom",
    displayName: "Custom SMTP",
    host: "",
    port: 587,
    tlsMode: "starttls",
  },
  {
    id: "fastmail",
    displayName: "Fastmail",
    host: "smtp.fastmail.com",
    port: 465,
    tlsMode: "tls",
  },
  {
    id: "outlook",
    displayName: "Outlook / Microsoft 365",
    host: "smtp-mail.outlook.com",
    port: 587,
    tlsMode: "starttls",
  },
  {
    id: "yahoo",
    displayName: "Yahoo Mail",
    host: "smtp.mail.yahoo.com",
    port: 587,
    tlsMode: "starttls",
  },
] as const satisfies readonly SMTPPreset[];

export const GMAIL_APP_PASSWORD_ENDPOINT = {
  host: "smtp.gmail.com",
  port: 587,
  tlsMode: "starttls",
} as const;
