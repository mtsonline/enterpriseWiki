// Ready-made process templates for the workflow editor.
import type { Definition } from './types';

const hr: Record<'en' | 'de', Definition> = {
  en: {
    description: 'Apply for an open position. HR checks your application and gets back to you; you can follow the status of your application here at any time.',
    fields: [
      { id: 'name', label: 'Full name', type: 'text', required: true },
      { id: 'email', label: 'E-mail', type: 'email', required: true },
      { id: 'phone', label: 'Phone', type: 'text' },
      { id: 'position', label: 'Position', type: 'select', required: true, options: ['Software developer', 'Project manager', 'Office assistant', 'Unsolicited application'] },
      { id: 'start_date', label: 'Earliest start date', type: 'date' },
      { id: 'cv', label: 'CV (PDF)', type: 'file', required: true },
      { id: 'letter', label: 'Cover letter', type: 'textarea' },
      { id: 'privacy', label: 'I agree that my data is processed for this application.', type: 'checkbox', required: true },
    ],
    steps: [
      { id: 'confirmation', name: 'Confirmation of receipt', type: 'email', assignee: { type: '', value: '' }, actions: [], next: 'screening',
        email: { to: '{{field.email}}', subject: 'Your application: {{field.position}}', body: 'Dear {{field.name}},\n\nthank you for your application as {{field.position}}. We have received your documents and will get back to you as soon as possible.\n\nKind regards\nHuman Resources' } },
      { id: 'screening', name: 'HR screening', description: 'HR checks the documents.', assignee: { type: 'group', value: 'hr' }, due_days: 5,
        actions: [{ id: 'invite', label: 'Invite to interview', next: 'interview' }, { id: 'reject', label: 'Reject', next: 'rejection_mail', note: 'required' }] },
      { id: 'interview', name: 'Interview', description: 'The hiring manager meets the applicant.', assignee: { type: 'role', value: 'hiring-manager' },
        actions: [{ id: 'offer', label: 'Make an offer', next: 'offer', note: 'optional' }, { id: 'reject', label: 'Reject', next: 'rejection_mail', note: 'required' }] },
      { id: 'rejection_mail', name: 'Rejection letter', type: 'email', assignee: { type: '', value: '' }, actions: [], next: 'rejected',
        email: { to: '{{field.email}}', subject: 'Your application: {{field.position}}', body: 'Dear {{field.name}},\n\nthank you for your interest. Unfortunately we have decided in favour of other candidates.\n\nWe wish you all the best.\n\n{{actor.name}}\nHuman Resources' } },
      { id: 'offer', name: 'Offer', description: 'HR sends the contract and waits for the answer.', assignee: { type: 'group', value: 'hr' },
        actions: [{ id: 'accepted', label: 'Offer accepted', next: 'hired' }, { id: 'declined', label: 'Offer declined', next: 'withdrawn', note: 'optional' }] },
      { id: 'hired', name: 'Hired', actions: [], outcome: 'success', assignee: { type: 'starter', value: '' } },
      { id: 'rejected', name: 'Rejected', actions: [], outcome: 'failure', assignee: { type: 'starter', value: '' } },
      { id: 'withdrawn', name: 'Withdrawn', actions: [], outcome: 'neutral', assignee: { type: 'starter', value: '' } },
    ],
  },
  de: {
    description: 'Bewerben Sie sich auf eine offene Stelle. Die Personalabteilung prüft Ihre Bewerbung und meldet sich bei Ihnen; den Stand Ihrer Bewerbung sehen Sie jederzeit hier.',
    fields: [
      { id: 'name', label: 'Vor- und Nachname', type: 'text', required: true },
      { id: 'email', label: 'E-Mail', type: 'email', required: true },
      { id: 'phone', label: 'Telefon', type: 'text' },
      { id: 'position', label: 'Stelle', type: 'select', required: true, options: ['Softwareentwicklung', 'Projektleitung', 'Büroassistenz', 'Initiativbewerbung'] },
      { id: 'start_date', label: 'Frühester Eintritt', type: 'date' },
      { id: 'cv', label: 'Lebenslauf (PDF)', type: 'file', required: true },
      { id: 'letter', label: 'Anschreiben', type: 'textarea' },
      { id: 'privacy', label: 'Ich stimme der Verarbeitung meiner Daten für diese Bewerbung zu.', type: 'checkbox', required: true },
    ],
    steps: [
      { id: 'confirmation', name: 'Eingangsbestätigung', type: 'email', assignee: { type: '', value: '' }, actions: [], next: 'screening',
        email: { to: '{{field.email}}', subject: 'Ihre Bewerbung: {{field.position}}', body: 'Guten Tag {{field.name}},\n\nvielen Dank für Ihre Bewerbung als {{field.position}}. Wir haben Ihre Unterlagen erhalten und melden uns so bald wie möglich bei Ihnen.\n\nMit freundlichen Grüßen\nIhre Personalabteilung' } },
      { id: 'screening', name: 'Vorauswahl Personal', description: 'Die Personalabteilung prüft die Unterlagen.', assignee: { type: 'group', value: 'hr' }, due_days: 5,
        actions: [{ id: 'invite', label: 'Zum Gespräch einladen', next: 'interview' }, { id: 'reject', label: 'Absagen', next: 'rejection_mail', note: 'required' }] },
      { id: 'interview', name: 'Vorstellungsgespräch', description: 'Die Fachabteilung lernt die Person kennen.', assignee: { type: 'role', value: 'hiring-manager' },
        actions: [{ id: 'offer', label: 'Angebot machen', next: 'offer', note: 'optional' }, { id: 'reject', label: 'Absagen', next: 'rejection_mail', note: 'required' }] },
      { id: 'rejection_mail', name: 'Absageschreiben', type: 'email', assignee: { type: '', value: '' }, actions: [], next: 'rejected',
        email: { to: '{{field.email}}', subject: 'Ihre Bewerbung: {{field.position}}', body: 'Guten Tag {{field.name}},\n\nvielen Dank für Ihr Interesse. Leider haben wir uns für andere Bewerbungen entschieden.\n\nWir wünschen Ihnen alles Gute.\n\n{{actor.name}}\nPersonalabteilung' } },
      { id: 'offer', name: 'Vertragsangebot', description: 'Die Personalabteilung schickt den Vertrag und wartet auf die Antwort.', assignee: { type: 'group', value: 'hr' },
        actions: [{ id: 'accepted', label: 'Angebot angenommen', next: 'hired' }, { id: 'declined', label: 'Angebot abgelehnt', next: 'withdrawn', note: 'optional' }] },
      { id: 'hired', name: 'Eingestellt', actions: [], outcome: 'success', assignee: { type: 'starter', value: '' } },
      { id: 'rejected', name: 'Abgesagt', actions: [], outcome: 'failure', assignee: { type: 'starter', value: '' } },
      { id: 'withdrawn', name: 'Zurückgezogen', actions: [], outcome: 'neutral', assignee: { type: 'starter', value: '' } },
    ],
  },
};

const approval: Record<'en' | 'de', Definition> = {
  en: {
    description: 'Request an approval. Your manager decides; you can follow the status here.',
    fields: [{ id: 'subject', label: 'Subject', type: 'text', required: true }, { id: 'details', label: 'Details', type: 'textarea', required: true }],
    steps: [
      { id: 'decision', name: 'Decision', type: 'approval', assignee: { type: 'role', value: 'manager' }, actions: [], required: 1, due_days: 3, next: 'approved', next_else: 'rejected' },
      { id: 'approved', name: 'Approved', actions: [], outcome: 'success', assignee: { type: 'starter', value: '' } },
      { id: 'rejected', name: 'Rejected', actions: [], outcome: 'failure', assignee: { type: 'starter', value: '' } },
    ],
  },
  de: {
    description: 'Stellen Sie einen Genehmigungsantrag. Ihre Führungskraft entscheidet; den Stand sehen Sie hier.',
    fields: [{ id: 'subject', label: 'Betreff', type: 'text', required: true }, { id: 'details', label: 'Details', type: 'textarea', required: true }],
    steps: [
      { id: 'decision', name: 'Entscheidung', type: 'approval', assignee: { type: 'role', value: 'manager' }, actions: [], required: 1, due_days: 3, next: 'approved', next_else: 'rejected', approve_label: 'Genehmigen', reject_label: 'Ablehnen' },
      { id: 'approved', name: 'Genehmigt', actions: [], outcome: 'success', assignee: { type: 'starter', value: '' } },
      { id: 'rejected', name: 'Abgelehnt', actions: [], outcome: 'failure', assignee: { type: 'starter', value: '' } },
    ],
  },
};

const onboarding: Record<'en' | 'de', Definition> = {
  en: {
    description: 'Prepare the first working day of a new colleague.',
    fields: [
      { id: 'name', label: 'Name', type: 'text', required: true }, { id: 'email', label: 'Private e-mail', type: 'email', required: true },
      { id: 'start_date', label: 'First working day', type: 'date', required: true }, { id: 'team', label: 'Team', type: 'text' },
    ],
    steps: [
      { id: 'it', name: 'IT equipment and accounts', type: 'task', assignee: { type: 'group', value: 'it' }, due_days: 3, actions: [{ id: 'done', label: 'Ready', next: 'wait' }] },
      { id: 'wait', name: 'Until the first day', type: 'delay', assignee: { type: '', value: '' }, actions: [], delay: { until: 'start_date', hours: 7 }, next: 'welcome' },
      { id: 'welcome', name: 'Welcome e-mail', type: 'email', assignee: { type: '', value: '' }, actions: [], next: 'done',
        email: { to: '{{field.email}}', cc: 'starter', subject: 'Welcome to {{site.name}}!', body: 'Hello {{field.name}},\n\nwelcome to the team {{field.team}}! We are looking forward to working with you.\n\n{{starter.name}}' } },
      { id: 'done', name: 'Onboarding started', type: 'end', assignee: { type: '', value: '' }, actions: [], outcome: 'success' },
    ],
  },
  de: {
    description: 'Den ersten Arbeitstag einer neuen Kollegin oder eines neuen Kollegen vorbereiten.',
    fields: [
      { id: 'name', label: 'Name', type: 'text', required: true }, { id: 'email', label: 'Private E-Mail', type: 'email', required: true },
      { id: 'start_date', label: 'Erster Arbeitstag', type: 'date', required: true }, { id: 'team', label: 'Team', type: 'text' },
    ],
    steps: [
      { id: 'it', name: 'IT-Ausstattung und Zugänge', type: 'task', assignee: { type: 'group', value: 'it' }, due_days: 3, actions: [{ id: 'done', label: 'Bereit', next: 'wait' }] },
      { id: 'wait', name: 'Bis zum ersten Tag', type: 'delay', assignee: { type: '', value: '' }, actions: [], delay: { until: 'start_date', hours: 7 }, next: 'welcome' },
      { id: 'welcome', name: 'Willkommens-E-Mail', type: 'email', assignee: { type: '', value: '' }, actions: [], next: 'done',
        email: { to: '{{field.email}}', cc: 'starter', subject: 'Willkommen bei {{site.name}}!', body: 'Hallo {{field.name}},\n\nwillkommen im Team {{field.team}}! Wir freuen uns auf die Zusammenarbeit.\n\n{{starter.name}}' } },
      { id: 'done', name: 'Einarbeitung gestartet', type: 'end', assignee: { type: '', value: '' }, actions: [], outcome: 'success' },
    ],
  },
};

export const TEMPLATES: { id: string; def: (lang: string) => Definition }[] = [
  { id: 'hr', def: (l) => structuredClone(hr[l === 'de' ? 'de' : 'en']) },
  { id: 'approval', def: (l) => structuredClone(approval[l === 'de' ? 'de' : 'en']) },
  { id: 'onboarding', def: (l) => structuredClone(onboarding[l === 'de' ? 'de' : 'en']) },
];
