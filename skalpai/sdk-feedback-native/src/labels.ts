export type FeedbackLang = 'fr' | 'en';

export type Labels = {
  title: string;
  send: string;
  sending: string;
  close: string;
  bug: string;
  idea: string;
  other: string;
  placeholder: string;
  placeholder_bug: string;
  thanks: string;
  capture: string;
  capturing: string;
  attach: string;
  remove_attachment: string;
  email_placeholder: string;
  email_invalid: string;
  hide: string;
  show: string;
  nudge: string;
  context_attached: string;
  too_many_attachments: string;
  attachment_too_large: string;
  attachment_unsupported: string;
  send_failed: string;
};

const FR_LABELS: Labels = {
  title: 'Votre avis',
  send: 'Envoyer',
  sending: 'Envoi…',
  close: 'Fermer',
  bug: 'Bug',
  idea: 'Avis ou idée',
  other: 'Autre',
  placeholder: 'Ce qui vous plaît, ce qui manque, une idée…',
  placeholder_bug: 'Qu’est-ce qui ne marche pas ? Que faisiez-vous ?',
  thanks: 'Merci pour votre retour !',
  capture: 'Capturer l’écran',
  capturing: 'Capture…',
  attach: 'Joindre une image',
  remove_attachment: 'Retirer la pièce jointe',
  email_placeholder: 'Votre email (pour une réponse)',
  email_invalid: 'Email invalide',
  hide: 'Masquer le bouton',
  show: 'Afficher le bouton avis',
  nudge: 'Un souci ? Dites-le-nous',
  context_attached: 'Détails techniques joints',
  too_many_attachments: 'Maximum {max} pièces jointes',
  attachment_too_large: 'Fichier trop volumineux (max {max} Mo)',
  attachment_unsupported: 'Type de fichier non supporté',
  send_failed: 'L’envoi a échoué, réessayez',
};

const EN_LABELS: Labels = {
  title: 'Feedback',
  send: 'Send',
  sending: 'Sending…',
  close: 'Close',
  bug: 'Bug',
  idea: 'Opinion or idea',
  other: 'Other',
  placeholder: 'What you like, what’s missing, an idea…',
  placeholder_bug: 'What isn’t working? What were you doing?',
  thanks: 'Thanks for your feedback!',
  capture: 'Capture screen',
  capturing: 'Capturing…',
  attach: 'Attach an image',
  remove_attachment: 'Remove attachment',
  email_placeholder: 'Your email (to get a reply)',
  email_invalid: 'Invalid email',
  hide: 'Hide the button',
  show: 'Show the feedback button',
  nudge: 'Something wrong? Tell us',
  context_attached: 'Technical details attached',
  too_many_attachments: 'At most {max} attachments',
  attachment_too_large: 'File too large (max {max} MB)',
  attachment_unsupported: 'Unsupported file type',
  send_failed: 'Sending failed, please retry',
};

export function resolveLang(lang: string | undefined): FeedbackLang {
  return lang?.toLowerCase().startsWith('en') ? 'en' : 'fr';
}

export function resolveLabels(lang: string | undefined, overrides?: Partial<Labels>): Labels {
  const base = resolveLang(lang) === 'en' ? EN_LABELS : FR_LABELS;
  return overrides ? { ...base, ...overrides } : base;
}

export function fillMax(template: string, max: number): string {
  return template.replace('{max}', String(max));
}
