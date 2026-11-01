export interface Field { id: string; label: string; type: string; required?: boolean; options?: string[]; help?: string }
export interface Assignee { type: 'starter' | 'user' | 'group' | 'role' | ''; value: string }
export interface Action { id: string; label: string; next: string; note?: '' | 'optional' | 'required' }
export interface Rule { field: string; op: string; value?: string }
export interface EmailSpec { to: string; cc?: string; from?: string; reply_to?: string; subject: string; body: string }
export interface WebhookSpec { url: string; method?: string; secret?: string }
export interface DelaySpec { days?: number; hours?: number; until?: string }
export type StepType = 'task' | 'approval' | 'condition' | 'email' | 'webhook' | 'delay' | 'end';
export interface Step {
  id: string; name: string; type?: StepType; description?: string; assignee: Assignee; actions: Action[]; outcome?: string;
  due_days?: number; required?: number; approve_label?: string; reject_label?: string; next?: string; next_else?: string;
  match?: 'all' | 'any'; rules?: Rule[]; email?: EmailSpec; webhook?: WebhookSpec; delay?: DelaySpec;
}
export interface Definition { description: string; fields: Field[]; steps: Step[] }

export interface Event { id: number; from_step: string | null; to_step: string | null; action: string; action_label: string; note: string; actor: string; at: string }
export interface Instance {
  id: number; element_id: number; category_id: number; element_titles: Record<string, string>; lang: string; title: string;
  data: Record<string, unknown>; definition: Definition; step: string; step_name: string; status: 'open' | 'done' | 'cancelled'; outcome?: string;
  started_by: string; started_at: string; updated_at: string; closed_at: string | null; actions: Action[]; can_cancel: boolean; events?: Event[];
  progress?: string; last_error?: string; wake_at?: string | null;
}
export interface Meta { step_types: StepType[]; rule_ops: string[]; placeholders: string[]; field_types: string[]; mail_configured: boolean; http_allow: boolean }
