import { useEffect, useState } from "react";

import { readViewport, type ViewportMetrics } from "./viewport";

const SERVER: ViewportMetrics = { width: 0, height: 0, offsetTop: 0, bottomInset: 0 };

function read(): ViewportMetrics {
  if (typeof window === "undefined") return SERVER;
  return readViewport({
    innerWidth: window.innerWidth,
    innerHeight: window.innerHeight,
    visual: window.visualViewport,
  });
}

/**
 * useViewport tracks the area a fixed element can occupy, keyboard included.
 *
 * The visual viewport's own events are the only ones a raised keyboard fires:
 * `window.resize` does not run on iOS when the keyboard opens, so a component
 * listening to it alone keeps the measurements it took before the keys
 * appeared.
 */
export function useViewport(): ViewportMetrics {
  const [metrics, setMetrics] = useState<ViewportMetrics>(read);

  useEffect(() => {
    const update = () => setMetrics(read());
    update();

    const visual = window.visualViewport;
    visual?.addEventListener("resize", update);
    // iOS scrolls the visual viewport rather than the document to keep the
    // caret above the keyboard, and reports it here and nowhere else.
    visual?.addEventListener("scroll", update);
    window.addEventListener("resize", update);
    window.addEventListener("orientationchange", update);

    return () => {
      visual?.removeEventListener("resize", update);
      visual?.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
      window.removeEventListener("orientationchange", update);
    };
  }, []);

  return metrics;
}
