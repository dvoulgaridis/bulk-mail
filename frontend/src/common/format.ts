import type { SMTPProfile } from "../api/types";

export function shortDate(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function placeholderToken(key: string): string {
  return `{{${key}}}`;
}

export function senderLabel(profile: SMTPProfile): string {
  return `${profile.senderName || profile.name || "Sender"} <${profile.senderEmail || profile.username || "not set"}>`;
}

export function smtpLabel(profile: SMTPProfile): string {
  return profile.profileType === "gmail_oauth" ? "Gmail API" : `${profile.host}:${profile.port}`;
}

export function transportLabel(profile: SMTPProfile): string {
  return profile.profileType === "gmail_oauth" ? "gmail oauth" : "smtp";
}
