import { describe, expect, it, vi } from 'vitest';
import { createRageTapDetector } from './rage';

describe('createRageTapDetector', () => {
  it('fires on three clustered taps within the window', () => {
    const onRage = vi.fn();
    const tap = createRageTapDetector(onRage);
    tap({ t: 0, x: 100, y: 100 });
    tap({ t: 200, x: 105, y: 102 });
    tap({ t: 400, x: 98, y: 110 });
    expect(onRage).toHaveBeenCalledTimes(1);
  });

  it('ignores taps that are spread out in space or time', () => {
    const onRage = vi.fn();
    const tap = createRageTapDetector(onRage);
    tap({ t: 0, x: 0, y: 0 });
    tap({ t: 100, x: 200, y: 0 });
    tap({ t: 200, x: 0, y: 200 });
    tap({ t: 2000, x: 0, y: 0 });
    tap({ t: 3000, x: 0, y: 0 });
    expect(onRage).not.toHaveBeenCalled();
  });
});
