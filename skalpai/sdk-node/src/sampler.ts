import { SamplingDecision } from '@opentelemetry/sdk-trace-base';
import type { Sampler, SamplingResult } from '@opentelemetry/sdk-trace-base';
import { ParentBasedSampler } from '@opentelemetry/sdk-trace-base';
import type { Attributes, Context, Link, SpanKind } from '@opentelemetry/api';

export const DEFAULT_EXCLUDED_PATHS = ['/healthcheck'];

const ROUTE_ATTRIBUTES = [
  'http.route',
  'http.target',
  'url.path',
  'next.route',
] as const;

function candidatePaths(spanName: string, attributes: Attributes): string[] {
  const paths: string[] = [];

  for (const key of ROUTE_ATTRIBUTES) {
    const value = attributes[key];
    if (typeof value === 'string' && value) paths.push(value);
  }

  // Next.js names its root server span "GET /healthcheck" before any route
  // attribute is set, so the span name is the only signal available at
  // sampling time.
  const fromName = spanName.includes(' ') ? spanName.slice(spanName.indexOf(' ') + 1) : spanName;
  if (fromName) paths.push(fromName);

  return paths;
}

function normalize(path: string): string {
  const withoutQuery = path.split(/[?#]/)[0];
  const trimmed = withoutQuery.replace(/\/+$/, '');
  return trimmed || '/';
}

function isExcluded(paths: string[], excluded: string[]): boolean {
  return paths.some((path) => excluded.includes(normalize(path)));
}

class PathExcludingSampler implements Sampler {
  private readonly excluded: string[];

  constructor(excludePaths: string[]) {
    this.excluded = excludePaths.map(normalize);
  }

  shouldSample(
    _context: Context,
    _traceId: string,
    spanName: string,
    _spanKind: SpanKind,
    attributes: Attributes,
    _links: Link[],
  ): SamplingResult {
    const paths = candidatePaths(spanName, attributes);
    return isExcluded(paths, this.excluded)
      ? { decision: SamplingDecision.NOT_RECORD }
      : { decision: SamplingDecision.RECORD_AND_SAMPLED };
  }

  toString(): string {
    return `PathExcludingSampler{${this.excluded.join(',')}}`;
  }
}

export function createSampler(excludePaths: string[]): Sampler {
  return new ParentBasedSampler({
    root: new PathExcludingSampler(excludePaths),
  });
}
