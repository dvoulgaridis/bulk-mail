import { provide } from "vue";
import { workspaceKey } from "./app/context";
import { createWorkspace } from "./app/workspace";
import { provideAddressListsFeature } from "./features/address-lists/useAddressLists";
import { provideCampaignsFeature } from "./features/campaigns/useCampaigns";
import { provideProfilesFeature } from "./features/profiles/useProfiles";
import { provideReportsFeature } from "./features/reports/useReports";
import { provideSettingsFeature } from "./features/settings/useSettings";

export function provideApplication() {
  const workspace = createWorkspace();
  provide(workspaceKey, workspace);
  const profiles = provideProfilesFeature(workspace);
  provideAddressListsFeature(workspace);
  const reports = provideReportsFeature(workspace);
  provideCampaignsFeature(workspace, reports.selectTask);
  const settings = provideSettingsFeature(workspace);
  return { workspace, profiles, settings };
}
