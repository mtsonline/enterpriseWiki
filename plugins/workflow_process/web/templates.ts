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
      { id: 'screening', name: 'HR screening', description: 'HR checks the documents.', assignee: { type: 'group', value: 'hr' },
        actions: [{ id: 'invite', label: 'Invite to interview', next: 'interview' }, { id: 'reject', label: 'Reject', next: 'rejected', note: 'required' }] },
      { id: 'interview', name: 'Interview', description: 'The hiring manager meets the applicant.', assignee: { type: 'role', value: 'hiring-manager' },
        actions: [{ id: 'offer', label: 'Make an offer', next: 'offer', note: 'optional' }, { id: 'reject', label: 'Reject', next: 'rejected', note: 'required' }] },
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
      { id: 'screening', name: 'Vorauswahl Personal', description: 'Die Personalabteilung prüft die Unterlagen.', assignee: { type: 'group', value: 'hr' },
        actions: [{ id: 'invite', label: 'Zum Gespräch einladen', next: 'interview' }, { id: 'reject', label: 'Absagen', next: 'rejected', note: 'required' }] },
      { id: 'interview', name: 'Vorstellungsgespräch', description: 'Die Fachabteilung lernt die Person kennen.', assignee: { type: 'role', value: 'hiring-manager' },
        actions: [{ id: 'offer', label: 'Angebot machen', next: 'offer', note: 'optional' }, { id: 'reject', label: 'Absagen', next: 'rejected', note: 'required' }] },
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
      { id: 'decision', name: 'Decision', assignee: { type: 'role', value: 'manager' },
        actions: [{ id: 'approve', label: 'Approve', next: 'approved', note: 'optional' }, { id: 'reject', label: 'Reject', next: 'rejected', note: 'required' }] },
      { id: 'approved', name: 'Approved', actions: [], outcome: 'success', assignee: { type: 'starter', value: '' } },
      { id: 'rejected', name: 'Rejected', actions: [], outcome: 'failure', assignee: { type: 'starter', value: '' } },
    ],
  },
  de: {
    description: 'Stellen Sie einen Genehmigungsantrag. Ihre Führungskraft entscheidet; den Stand sehen Sie hier.',
    fields: [{ id: 'subject', label: 'Betreff', type: 'text', required: true }, { id: 'details', label: 'Details', type: 'textarea', required: true }],
    steps: [
      { id: 'decision', name: 'Entscheidung', assignee: { type: 'role', value: 'manager' },
        actions: [{ id: 'approve', label: 'Genehmigen', next: 'approved', note: 'optional' }, { id: 'reject', label: 'Ablehnen', next: 'rejected', note: 'required' }] },
      { id: 'approved', name: 'Genehmigt', actions: [], outcome: 'success', assignee: { type: 'starter', value: '' } },
      { id: 'rejected', name: 'Abgelehnt', actions: [], outcome: 'failure', assignee: { type: 'starter', value: '' } },
    ],
  },
};

export const TEMPLATES: { id: string; def: (lang: string) => Definition }[] = [
  { id: 'hr', def: (l) => structuredClone(hr[l === 'de' ? 'de' : 'en']) },
  { id: 'approval', def: (l) => structuredClone(approval[l === 'de' ? 'de' : 'en']) },
];
