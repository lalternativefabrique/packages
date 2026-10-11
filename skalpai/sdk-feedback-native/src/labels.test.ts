import { describe, expect, it } from 'vitest';
import { fillMax, resolveLabels } from './labels';

describe('resolveLabels', () => {
  it('defaults to French and picks English from a locale tag', () => {
    expect(resolveLabels(undefined).send).toBe('Envoyer');
    expect(resolveLabels('en-US').send).toBe('Send');
  });

  it('applies overrides on top of the language set', () => {
    expect(resolveLabels('fr', { title: 'Un retour ?' }).title).toBe('Un retour ?');
  });

  it('fills the max placeholder', () => {
    expect(fillMax(resolveLabels('en').too_many_attachments, 5)).toBe('At most 5 attachments');
  });
});
