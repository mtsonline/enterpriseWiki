export interface Field { id: string; label: string; type: string; required?: boolean; options?: string[]; help?: string; later?: boolean }
export interface StepField { field: string; mode: 'edit' | 'required' | 'read' | 'hidden' }
export interface ParallelSpec { branches: string[]; require?: number; for_each?: string }
export interface Assignee { type: 'starter' | 'user' | 'group' | 'role' | ''; value: string }
export interface Action { id: string; label: string; next: string; note?: '' | 'optional' | 'required' }
export interface Rule { field: string; op: string; value?: string }
export interface EmailSpec { to: string; cc?: string; from?: string; reply_to?: string; subject: string; body: string }
export interface WebhookSpec { url: string; method?: string; secret?: string; secret_ref?: string }
export interface DelaySpec { days?: number; hours?: number; until?: string }
export type StepType = 'task' | 'approval' | 'condition' | 'email' | 'notify' | 'set' | 'webhook' | 'http' | 'create' | 'props' | 'access' | 'membership' | 'move' | 'subflow' | 'parallel' | 'delay' | 'end';
export type Channel = 'inbox' | 'email' | 'both';
export interface NotifySpec { to: string; subject: string; body?: string; channel?: Channel }
export interface Notice extends NotifySpec { on: 'enter' | 'leave' }
export interface Assign { field: string; value?: string; formula?: string }
export interface HTTPSpec { url: string; method?: string; body?: string; map?: Record<string, string> }
export interface CreateSpec { category: number; title: string; body?: string; tags?: string; store_id?: string; store_url?: string }
export interface PropsSpec { element: string; values: Record<string, string> }
export interface AccessSpec { mode: 'grant' | 'revoke'; category: number; subject: 'user' | 'group'; user?: string; group?: number; level?: 'reader' | 'editor' | 'manager'; days?: number }
export interface MemberSpec { mode: 'add' | 'remove'; group: number; user: string }
export interface MoveSpec { element: string; category: number }
export interface SubflowSpec { workflow: number; title?: string; data?: Record<string, string>; wait?: boolean; return?: Record<string, string> }
export interface Escalation { action: '' | 'notify' | 'reassign' | 'action' | 'cancel'; notify?: NotifySpec; assignee?: Assignee; action_id?: string }
export interface Deadline { days?: number; hours?: number; field?: string; workdays?: boolean; warn?: number[]; escalate?: Escalation }
export type TriggerType = 'schedule' | 'content' | 'event' | 'webhook';
export interface Trigger {
  id: string; type: TriggerType; name?: string; off?: boolean; title?: string; data?: Record<string, string>; run_as?: 'definer' | 'actor';
  cron?: string; events?: string[]; sources?: string; prop?: Rule; event?: string; category?: number; workflow?: number; outcome?: string; allow_from?: string[]; group?: number;
}
export interface TriggerStatus { id: string; type: TriggerType; name?: string; active: boolean; has_secret?: boolean; url?: string; next?: string; last_at?: string; last_error?: string; starts_24h: number }
export interface Step {
  id: string; name: string; type?: StepType; description?: string; assignee: Assignee; actions: Action[]; outcome?: string;
  due_days?: number; required?: number; approve_label?: string; reject_label?: string; next?: string; next_else?: string;
  match?: 'all' | 'any'; rules?: Rule[]; email?: EmailSpec; webhook?: WebhookSpec; delay?: DelaySpec;
  notify?: NotifySpec; set?: Assign[]; http?: HTTPSpec; create?: CreateSpec; props?: PropsSpec; subflow?: SubflowSpec; access?: AccessSpec; membership?: MemberSpec; move?: MoveSpec;
  run_as?: 'definer' | 'starter'; deadline?: Deadline; notices?: Notice[]; form?: StepField[]; parallel?: ParallelSpec;
}
export interface Definition { description: string; fields: Field[]; steps: Step[]; triggers?: Trigger[]; no_manual?: boolean; links?: boolean; link_hours?: number; retention?: { days: number; mode: 'delete' | 'anonymize' } }

export interface Event { id: number; from_step: string | null; to_step: string | null; action: string; action_label: string; note: string; actor: string; at: string }
export interface Instance {
  id: number; element_id: number; category_id: number; element_titles: Record<string, string>; lang: string; title: string;
  data: Record<string, unknown>; definition: Definition; step: string; step_name: string; status: 'open' | 'done' | 'cancelled'; outcome?: string;
  started_by: string; started_at: string; updated_at: string; closed_at: string | null; actions: Action[]; can_cancel: boolean; events?: Event[];
  progress?: string; last_error?: string; wake_at?: string | null; due_at?: string | null; escalated?: boolean;
  handover?: '' | 'claim' | 'delegate' | 'ask'; rev?: number; test?: boolean; form?: StepField[]; branches?: { id: number; title: string; step: string; step_name: string; status: string; outcome?: string }[];
}
export interface Meta {
  step_types: StepType[]; rule_ops: string[]; placeholders: string[]; field_types: string[]; mail_configured: boolean; http_allow: boolean;
  trigger_types?: TriggerType[]; content_events?: string[]; trigger_events?: string[]; max_depth?: number; per_hour?: number;
}
