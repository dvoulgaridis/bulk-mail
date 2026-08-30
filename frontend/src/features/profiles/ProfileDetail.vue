<script setup lang="ts">
import { onBeforeUnmount } from "vue";
import { useWorkspace } from "../../app/context";
import { useProfilesFeature } from "./useProfiles";

const { state } = useWorkspace();
const {
  draft,
  profileTypeChoice,
  smtpPresetID,
  customSMTPPresets,
  isGmailOAuth,
  hasSavedGoogleOAuth,
  hasSavedGmailAppPassword,
  hasSavedCurrentProfileType,
  passwordExists,
  passwordInputValue,
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
  clearSensitive,
  setPasswordFocused,
} = useProfilesFeature();

onBeforeUnmount(clearSensitive);
</script>

<template>
  <section class="bulk-mail-section">
    <form class="app-form bulk-mail-form" autocomplete="off" @submit.prevent="save">
      <fieldset class="bulk-mail-fieldset">
        <legend>Profile</legend>
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Profile name</span>
            <input v-model="draft.name" type="text" required />
          </label>
          <label class="app-form-field">
            <span>Profile type</span>
            <select :value="profileTypeChoice" required @change="changeProfileType">
              <option value="" disabled>Select profile type</option>
              <option value="smtp">Custom SMTP</option>
              <option value="gmail_oauth">Gmail (Connect)</option>
              <option value="gmail_app_password">Gmail (App password)</option>
            </select>
          </label>
        </div>
      </fieldset>

      <fieldset v-if="profileTypeChoice === 'smtp'" class="bulk-mail-fieldset">
        <legend>Sender identity</legend>
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Sender email</span>
            <input v-model="draft.senderEmail" type="email" required @input="syncSMTPUsername" />
          </label>
          <label class="app-form-field">
            <span>Display name</span>
            <input v-model="draft.senderName" type="text" />
          </label>
        </div>
        <label class="app-form-field"><span>Reply-to email</span><input v-model="draft.replyTo" type="email" /></label>
      </fieldset>

      <fieldset v-if="profileTypeChoice === 'smtp'" class="bulk-mail-fieldset">
        <legend>SMTP</legend>
        <div class="bulk-mail-advanced-fields">
          <div class="bulk-mail-advanced-group">
            <h3>SMTP preset</h3>
            <label class="app-form-field">
              <span>Preset</span>
              <select v-model="smtpPresetID" @change="applySMTPPreset(smtpPresetID)">
                <option value="" disabled>Select preset</option>
                <option
                  v-for="preset in customSMTPPresets"
                  :key="preset.id"
                  :value="preset.id"
                >
                  {{ preset.displayName }}
                </option>
              </select>
            </label>
          </div>
          <div class="bulk-mail-advanced-group">
            <h3>Credentials</h3>
            <div class="app-form-two-up">
              <label class="app-form-field">
                <span>SMTP username</span>
                <input v-model="draft.username" type="text" @input="markSMTPUsernameEdited" />
              </label>
              <label class="app-form-field">
                <span>SMTP password</span>
                <input
                  v-model="passwordInputValue"
                  type="password"
                  maxlength="1024"
                  autocomplete="new-password"
                  @focus="setPasswordFocused(true)"
                  @blur="setPasswordFocused(false)"
                />
              </label>
            </div>
          </div>
          <div class="bulk-mail-advanced-group bulk-mail-smtp-settings-area">
            <h3>Connection</h3>
            <div class="app-stage-actions app-stage-actions--start">
              <button type="button" @click="requestDetectSMTP">Detect settings</button>
            </div>
            <div class="app-form-two-up">
              <label class="app-form-field">
                <span>SMTP host</span>
                <input v-model="draft.host" type="text" required />
              </label>
              <label class="app-form-field">
                <span>SMTP port</span>
                <input
                  v-model.number="draft.port"
                  type="number"
                  min="1"
                  max="65535"
                  required
                  @change="normalizeConnection"
                />
              </label>
            </div>
            <div class="app-form-two-up">
              <label class="app-form-field">
                <span>Security</span>
                <select v-model="draft.tlsMode">
                  <option value="none">None</option>
                  <option value="starttls">STARTTLS</option>
                  <option value="tls">SSL</option>
                </select>
              </label>
              <label v-if="draft.id" class="app-form-field">
                <span>Test email address</span>
                <input v-model="draft.toEmail" type="email" />
              </label>
            </div>
          </div>
        </div>
      </fieldset>

      <fieldset
        v-if="profileTypeChoice === 'gmail_oauth' && hasSavedGoogleOAuth"
        class="bulk-mail-fieldset"
      >
        <legend>Sender identity</legend>
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Sender email</span>
            <input v-model="draft.senderEmail" type="email" readonly />
          </label>
          <label class="app-form-field">
            <span>Display name</span>
            <input v-model="draft.senderName" type="text" />
          </label>
        </div>
        <label class="app-form-field"><span>Reply-to email</span><input v-model="draft.replyTo" type="email" /></label>
      </fieldset>

      <fieldset v-if="profileTypeChoice === 'gmail_oauth'" class="bulk-mail-fieldset">
        <legend>Google connection</legend>
        <div v-if="hasSavedGoogleOAuth" class="bulk-mail-connection-panel">
          <dl class="bulk-mail-connection-details">
            <div><dt>Status</dt><dd>Connected</dd></div>
            <div><dt>Google account</dt><dd>{{ draft.senderEmail }}</dd></div>
            <div><dt>Transport</dt><dd>Gmail API</dd></div>
            <div><dt>API endpoint</dt><dd><code>{{ state.integrations.google.sendEndpoint }}</code></dd></div>
            <div><dt>Security</dt><dd>HTTPS</dd></div>
            <div><dt>Authorization</dt><dd>OAuth 2.0</dd></div>
            <div><dt>Permission</dt><dd>gmail.send</dd></div>
          </dl>
          <div class="app-stage-actions app-stage-actions--start">
            <button type="button" @click="startGoogleOAuth">Connect a different Google account</button>
          </div>
        </div>
        <div v-else class="bulk-mail-advanced-group">
          <p v-if="state.integrations.google.oauthConfigured">Google OAuth is configured for this application.</p>
          <p v-else>Configure a Google Desktop OAuth Client ID in <code>config.json</code> before connecting.</p>
          <p v-if="!state.integrations.google.oauthConfigured">
            <a
              href="https://console.cloud.google.com/apis/credentials"
              target="_blank"
              rel="noreferrer"
            >
              Create a Desktop OAuth Client ID
            </a>
          </p>
          <div class="app-stage-actions app-stage-actions--start">
            <button
              type="button"
              :disabled="!state.integrations.google.oauthConfigured"
              @click="startGoogleOAuth"
            >
              Continue to Google
            </button>
          </div>
        </div>
      </fieldset>

      <fieldset v-if="profileTypeChoice === 'gmail_app_password'" class="bulk-mail-fieldset">
        <legend>Gmail credentials</legend>
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Gmail address</span>
            <input v-model="draft.senderEmail" type="email" required @input="syncSMTPUsername" />
          </label>
          <label class="app-form-field">
            <span>App password</span>
            <input
              v-model="passwordInputValue"
              type="password"
              maxlength="1024"
              :required="!passwordExists"
              autocomplete="new-password"
              @focus="setPasswordFocused(true)"
              @blur="setPasswordFocused(false)"
            />
          </label>
        </div>
      </fieldset>

      <fieldset
        v-if="profileTypeChoice === 'gmail_app_password' && hasSavedGmailAppPassword"
        class="bulk-mail-fieldset"
      >
        <legend>Sender identity</legend>
        <div class="app-form-two-up">
          <label class="app-form-field">
            <span>Display name</span>
            <input v-model="draft.senderName" type="text" />
          </label>
          <label class="app-form-field">
            <span>Reply-to email</span>
            <input v-model="draft.replyTo" type="email" />
          </label>
        </div>
      </fieldset>

      <fieldset
        v-if="profileTypeChoice === 'gmail_app_password' && hasSavedGmailAppPassword"
        class="bulk-mail-fieldset"
      >
        <legend>SMTP</legend>
        <div class="bulk-mail-advanced-fields">
          <div class="bulk-mail-advanced-group">
            <h3>Credentials</h3>
            <label class="app-form-field">
              <span>SMTP username</span>
              <input
                v-model="draft.username"
                type="text"
                required
                @input="markSMTPUsernameEdited"
              />
            </label>
          </div>
          <div class="bulk-mail-advanced-group bulk-mail-smtp-settings-area">
            <h3>Connection</h3>
            <div class="app-form-two-up">
              <label class="app-form-field">
                <span>SMTP host</span>
                <input v-model="draft.host" type="text" required />
              </label>
              <label class="app-form-field">
                <span>SMTP port</span>
                <input
                  v-model.number="draft.port"
                  type="number"
                  min="1"
                  max="65535"
                  required
                  @change="normalizeConnection"
                />
              </label>
            </div>
            <div class="app-form-two-up">
              <label class="app-form-field">
                <span>Security</span>
                <select v-model="draft.tlsMode">
                  <option value="none">None</option>
                  <option value="starttls">STARTTLS</option>
                  <option value="tls">SSL</option>
                </select>
              </label>
              <label class="app-form-field">
                <span>Test email address</span>
                <input v-model="draft.toEmail" type="email" />
              </label>
            </div>
          </div>
        </div>
      </fieldset>

      <div v-if="profileTypeChoice" class="app-stage-actions">
        <button
          v-if="hasSavedCurrentProfileType && !isGmailOAuth"
          type="button"
          @click="test(false)"
        >
          Test connection
        </button>
        <button v-if="draft.id" type="button" @click="remove">Delete</button>
        <button
          v-if="hasSavedCurrentProfileType && !isGmailOAuth"
          type="button"
          @click="test(true)"
        >
          Send test email
        </button>
        <button v-if="!isGmailOAuth || hasSavedGoogleOAuth" type="submit" class="is-primary">Save</button>
      </div>
    </form>
  </section>
</template>
