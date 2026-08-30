import { inject, type InjectionKey, type Ref } from "vue";
import type { ApiClient } from "../api/client";
import type { Task, WorkspaceState } from "../api/types";

export type RouteName =
  | "dashboard"
  | "profiles"
  | "profile-detail"
  | "address-lists"
  | "address-list-detail"
  | "mapping"
  | "campaigns"
  | "new-campaign"
  | "suppressions"
  | "settings";

export type Notification = {
  id: number;
  text: string;
  type: "success" | "error";
};

export type WorkspaceContext = {
  api: ApiClient;
  currentView: Ref<RouteName>;
  currentViewLabel: Readonly<Ref<string>>;
  loading: Ref<boolean>;
  ready: Ref<boolean>;
  busy: Ref<boolean>;
  notifications: Ref<Notification[]>;
  state: WorkspaceState;
  bootstrap: () => Promise<void>;
  refresh: () => Promise<void>;
  mergeTasks: (tasks: Task[]) => void;
  disconnectTaskEvents: () => void;
  navigate: (route: RouteName) => void;
  notify: (text: string, type?: Notification["type"]) => void;
  dismissNotification: (id: number) => void;
  runAction: (action: () => Promise<void>) => Promise<void>;
};

export const workspaceKey: InjectionKey<WorkspaceContext> = Symbol("workspace");

export function useWorkspace(): WorkspaceContext {
  const workspace = inject(workspaceKey);
  if (!workspace) throw new Error("Workspace context is unavailable.");
  return workspace;
}
