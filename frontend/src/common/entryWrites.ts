import type { EntryOutcome, EntryWriteResult } from "../api/types";
import type { WorkspaceContext } from "../app/context";

export function entryWriteSucceeded(outcome: EntryOutcome): boolean {
  return ["inserted", "updated", "deleted"].includes(outcome.status);
}

export function notifyEntryWrites(workspace: WorkspaceContext, result: EntryWriteResult): void {
  const rejected = result.results.filter((outcome) => !entryWriteSucceeded(outcome));
  const completed = result.results.length - rejected.length;
  const details = rejected.slice(0, 3)
    .map((outcome) => `Entry ${outcome.index + 1}: ${outcome.message || outcome.status}`).join("; ");
  workspace.notify(
    `${completed} saved; ${rejected.length} not saved.${details ? ` ${details}` : ""}`,
    rejected.length ? "error" : "success",
  );
}
