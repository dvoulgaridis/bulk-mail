<script setup lang="ts">
import { computed } from "vue";
import { useWorkspace } from "../../app/context";
import { placeholderToken } from "../../common/format";
import AttachmentPicker from "../attachments/AttachmentPicker.vue";
import PersonalizationOptions from "./PersonalizationOptions.vue";
import { useCampaignsFeature } from "./useCampaigns";

const { state } = useWorkspace();
const {
  campaign,
  sampleAddressEntryID,
  preflightResult,
  preflightCurrent,
  unresolvedConfirmed,
  sampleAddressEntries,
  canRun,
  saveCampaign,
  deleteCampaign,
  runPreflight,
  run,
  viewSampleAttachment,
  transportLabel,
  formatFileSize,
} = useCampaignsFeature();
const availableFields = computed(() => {
  const definitions = state.addressLists.find((list) => list.id === campaign.addressListId)?.fields
    ?? state.addressFieldDefaults;
  const fields = [...definitions]
    .sort((left, right) => left.position - right.position || left.key.localeCompare(right.key))
    .map(({ key, label, role }) => ({ key, label, role }));
  const fullName = { key: "full_name", label: "Full name", role: "" as const };
  const lastNameIndex = fields.findIndex((field) => field.role === "last_name");
  return lastNameIndex < 0
    ? [...fields, fullName]
    : [...fields.slice(0, lastNameIndex + 1), fullName, ...fields.slice(lastNameIndex + 1)];
});

function sandboxedHTML(html: string): string {
  const policy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:";
  return `<meta http-equiv="Content-Security-Policy" content="${policy}">${html}`;
}
</script>

<template>
  <section class="bulk-mail-section">
    <form class="app-form bulk-mail-form" @submit.prevent="run('send')">
      <fieldset class="bulk-mail-fieldset">
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Profile</span>
            <select v-model.number="campaign.profileId" required>
              <option :value="null">Select profile</option>
              <option
                v-for="profile in state.smtpProfiles"
                :key="profile.id"
                :value="profile.id"
              >
                {{ profile.name }} [{{ transportLabel(profile) }}]
              </option>
            </select>
          </label>
          <label class="app-form-field">
            <span>Address list</span>
            <select v-model.number="campaign.addressListId" required>
              <option :value="0">Select address list</option>
              <option v-for="list in state.addressLists" :key="list.id" :value="list.id">
                {{ list.name }} ({{ list.count }})
              </option>
            </select>
          </label>
          <label v-if="sampleAddressEntries.length > 2" class="app-form-field">
            <span>Additional sample address entry</span>
            <select v-model.number="sampleAddressEntryID">
              <option :value="0">First and last only</option>
              <option v-for="entry in sampleAddressEntries" :key="entry.fields.email" :value="entry.id">
                {{ entry.displayName || entry.fields.email }} — {{ entry.fields.email }}
              </option>
            </select>
          </label>
        </div>
        <label class="app-form-field">
          <span>Campaign name</span>
          <input v-model="campaign.name" type="text" placeholder="April renewal notices" required />
        </label>
      </fieldset>

      <fieldset class="bulk-mail-fieldset">
        <label class="app-form-field">
          <span>Subject</span>
          <input v-model="campaign.message.subject" type="text" required />
        </label>
        <label class="app-form-field">
          <span>Message format</span>
          <select v-model="campaign.message.bodyFormat">
            <option value="text">Plain text</option>
            <option value="html">HTML</option>
          </select>
        </label>
        <label class="app-form-field">
          <span>Message body</span>
          <textarea v-model="campaign.message.body" rows="8"></textarea>
        </label>
        <PersonalizationOptions :options="campaign.personalization.message" />
      </fieldset>

      <div class="app-form-field bulk-mail-placeholders">
        <span>Placeholders</span>
        <div class="bulk-mail-placeholder-list" aria-label="Available placeholders">
          <code v-for="field in availableFields" :key="field.key" :title="field.label">
            {{ placeholderToken(field.key) }}
          </code>
        </div>
      </div>

      <AttachmentPicker />

      <fieldset class="bulk-mail-fieldset">
        <legend>Delivery notices</legend>
        <label class="app-checkbox-field">
          <input v-model="campaign.message.requestDeliveryNotice" type="checkbox" />
          <span>Request delivery/read receipt</span>
        </label>
      </fieldset>

      <div class="app-stage-actions">
        <button v-if="campaign.id > 0" type="button" class="is-danger" @click="deleteCampaign">
          Delete
        </button>
        <button type="button" @click="saveCampaign">Save</button>
        <button type="button" @click="runPreflight">Preview</button>
        <button type="button" :disabled="!canRun" @click="run('generate')">
          Generate only
        </button>
        <button type="submit" class="is-primary" :disabled="!canRun">
          Queue campaign
        </button>
      </div>
      <section v-if="preflightResult && preflightCurrent" class="bulk-mail-preview-panel">
        <div class="bulk-mail-section-head">
          <div>
            <h3>Preview</h3>
            <p>
              {{ preflightResult.count }} address entries ·
              {{ preflightResult.attachments.length }} attachments
            </p>
          </div>
        </div>

        <div v-if="preflightResult.attachments.length" class="bulk-mail-preflight-attachments">
          <div v-for="item in preflightResult.attachments" :key="item.filename">
            <strong>{{ item.filename }}</strong>
            <span v-if="item.convertedToPdf">
              {{
                item.placeholders.length
                  ? item.placeholders.map(placeholderToken).join(", ")
                  : "Converted to PDF without personalization"
              }}
            </span>
            <span v-else-if="item.placeholders.length">
              {{ item.placeholders.map(placeholderToken).join(", ") }}
            </span>
            <span v-else>Attached unchanged</span>
          </div>
        </div>

        <div v-if="preflightResult.unresolved.length" class="bulk-mail-preflight-warning">
          <strong>Unresolved personalization</strong>
          <ul>
            <li v-for="issue in preflightResult.unresolved" :key="issue.key + issue.reason">
              <code>{{ placeholderToken(issue.key) }}</code> —
              {{
                issue.reason === "missing_field"
                  ? "not defined in this address list"
                  : "empty for every address entry"
              }}
              ({{ issue.locations.join(", ") }})
            </li>
          </ul>
          <label class="app-checkbox-field">
            <input v-model="unresolvedConfirmed" type="checkbox" />
            <span>I confirm exactly the unresolved placeholders listed above.</span>
          </label>
        </div>

        <article
          v-for="sample in preflightResult.samples"
          :key="sample.addressEntryId"
          class="bulk-mail-preflight-sample"
        >
          <div class="bulk-mail-preview-header">
            <strong>{{ sample.name || sample.email }}</strong>
            <span>{{ sample.email }}</span>
          </div>
          <div class="bulk-mail-preview-message">
            <strong>{{ sample.subject }}</strong>
            <iframe
              v-if="sample.htmlBody"
              class="bulk-mail-html-preview"
              :sandbox="''"
              :srcdoc="sandboxedHTML(sample.htmlBody)"
              title="Personalized HTML message preview"
            ></iframe>
          </div>
          <div v-if="sample.attachments.length" class="bulk-mail-sample-files">
            <button
              v-for="attachment in sample.attachments"
              :key="attachment.filename"
              type="button"
              @click="viewSampleAttachment(attachment)"
            >
              {{ attachment.filename
              }}<template v-if="attachment.pageCount">
                · {{ attachment.pageCount }} page{{
                  attachment.pageCount === 1 ? "" : "s"
                }}</template
              >
              · {{ formatFileSize(attachment.size) }}
            </button>
          </div>
        </article>
      </section>
    </form>
  </section>
</template>
