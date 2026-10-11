import { describe, expect, it, vi } from 'vitest';
import { nudgeFeedback, openFeedback, reportProblem, subscribe } from './events';

describe('events', () => {
  it('delivers open and nudge to subscribers until they unsubscribe', () => {
    const listener = vi.fn();
    const unsubscribe = subscribe(listener);
    openFeedback({ type: 'idea' });
    nudgeFeedback();
    unsubscribe();
    openFeedback();
    expect(listener.mock.calls).toEqual([[{ kind: 'open', options: { type: 'idea' } }], [{ kind: 'nudge' }]]);
  });

  it('turns an error into a bug report with its stack', () => {
    const listener = vi.fn();
    const unsubscribe = subscribe(listener);
    const err = new TypeError('nope');
    reportProblem(err, { screen: 'home' });
    unsubscribe();
    const { options } = listener.mock.calls[0][0];
    expect(options.type).toBe('bug');
    expect(options.source).toBe('error');
    expect(options.context.screen).toBe('home');
    expect(options.context.error).toBe('TypeError: nope');
    expect(options.context.stack).toBe(err.stack);
  });

  it('stringifies non-Error values', () => {
    const listener = vi.fn();
    const unsubscribe = subscribe(listener);
    reportProblem('boom');
    unsubscribe();
    expect(listener.mock.calls[0][0].options.context).toEqual({ error: 'boom' });
  });
});
