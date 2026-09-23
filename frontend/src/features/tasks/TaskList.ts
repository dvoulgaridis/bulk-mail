import { shallowRef } from "vue";
import type { Task } from "../../api/types";

export class TaskList {
  #items = shallowRef<readonly Task[]>([]);

  get items(): readonly Task[] {
    return this.#items.value;
  }

  replaceAll(tasks: Task[]): void {
    const unique = new Map(tasks.map((task) => [task.id, task]));
    this.#items.value = Array.from(unique.values()).sort((left, right) => right.id - left.id);
  }

  upsert(tasks: Task[]): void {
    this.replaceAll([...this.#items.value, ...tasks]);
  }
}
