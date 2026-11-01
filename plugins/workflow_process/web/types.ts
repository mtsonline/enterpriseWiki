export interface Field { id: string; label: string; type: string; required?: boolean; options?: string[]; help?: string }
export interface Assignee { type: 'starter' | 'user' | 'group' | 'role'; value: string }
export interface Action { id: string; label: string; next: string; note?: '' | 'optional' | 'required' }
export interface Step { id: string; name: string; description?: string; assignee: Assignee; actions: Action[]; outcome?: string }
export interface Definition { description: string; fields: Field[]; steps: Step[] }

export interface Event { id: number; from_step: string | null; to_step: string | null; action: string; action_label: string; note: string; actor: string; at: string }
export interface Instance {
  id: number; element_id: number; category_id: number; element_titles: Record<string, string>; lang: string; title: string;
  data: Record<string, unknown>; definition: Definition; step: string; step_name: string; status: 'open' | 'done' | 'cancelled'; outcome?: string;
  started_by: string; started_at: string; updated_at: string; closed_at: string | null; actions: Action[]; can_cancel: boolean; events?: Event[];
}
