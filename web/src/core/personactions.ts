// The entries of plugins in the menu on names and on the person page
// (registerPersonAction). Without DOM: the registry and its rules only.

/** What the viewer may see of a person (GET /api/v1/people/{id}). */
export interface Person {
  id: number;
  display_name: string;
  handle: string;
  /** business card: job_title, department, phone, mobile, office, about, photo */
  card: Record<string, string>;
  /** only when the viewer may see it */
  email?: string;
  self: boolean;
}

/** An entry of plugins in the menu on names. */
export interface PersonAction {
  /** "<plugin id>.<name>", e.g. "chat.example.call" (never "core.") */
  id: string;
  /** the text of the entry (translated) */
  label: (p: Person) => string;
  /** shown only when true (what the viewer may do with this person) */
  visible?: (p: Person) => boolean;
  run: (p: Person) => void | Promise<void>;
}

const actions: PersonAction[] = [];
const ID_RE = /^[a-z0-9][a-z0-9_-]*(\.[a-z0-9_-]+)+$/;

/** Adds an entry of a plugin to the menu on names and the person page.
 * The id carries the plugin id (namespaces: "core." is the core's). */
export function registerPersonAction(a: PersonAction): void {
  if (!ID_RE.test(a.id) || a.id.startsWith('core.')) throw new Error(`person action ${a.id}: the id must be "<plugin id>.<name>"`);
  const i = actions.findIndex((x) => x.id === a.id);
  if (i >= 0) actions[i] = a;
  else actions.push(a);
}

/** The entries of plugins the viewer gets for person p. */
export function personActions(p: Person): PersonAction[] {
  return actions.filter((a) => {
    try { return a.visible ? a.visible(p) : true; } catch { return false; }
  });
}

