const RAGE_TAP_COUNT = 3;
const RAGE_TAP_WINDOW_MS = 800;
const RAGE_TAP_RADIUS = 30;

type Tap = { t: number; x: number; y: number };

export function createRageTapDetector(onRage: () => void): (tap: Tap) => void {
  let recent: Tap[] = [];
  return (tap) => {
    recent = recent.filter((c) => tap.t - c.t < RAGE_TAP_WINDOW_MS);
    recent.push(tap);
    const [first] = recent;
    const clustered = recent.every((c) => Math.hypot(c.x - first.x, c.y - first.y) < RAGE_TAP_RADIUS);
    if (recent.length >= RAGE_TAP_COUNT && clustered) {
      recent = [];
      onRage();
    }
  };
}
