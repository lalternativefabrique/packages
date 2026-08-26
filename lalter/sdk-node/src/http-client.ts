import axios, {
  type AxiosInstance,
  type AxiosRequestConfig,
  type AxiosResponse,
} from "axios";

export interface LalterClientOptions {
  /**
   * Base URL of the lalter API. No default — unlike Spore, lalter has no
   * single public deployment every consumer talks to.
   */
  baseURL: string;

  /**
   * The app key issued from lalter's console (Authorization: Bearer
   * <key>). Identifies the calling application; lalter resolves the user
   * it was issued for.
   */
  apiKey: string;

  /**
   * Optional custom axios instance. Use this to inject your own
   * interceptors, retry policy or telemetry. When provided, `baseURL` and
   * `apiKey` are ignored — wire those into your axios instance directly.
   */
  axios?: AxiosInstance;
}

let sharedClient: AxiosInstance | null = null;

// Kept alongside sharedClient, rather than read back off it, because axios
// does not put a header passed to axios.create() in a fixed, version-stable
// place (it lands on defaults.headers.Authorization on some versions,
// defaults.headers.common.Authorization on others). sendChatMessage needs the
// exact bearer value for its own fetch() call — reading it back from axios
// internals would tie this package to an implementation detail that already
// differs across axios releases, instead of the value this package itself
// set.
let sharedAuthHeader: string | undefined;

/**
 * Configure the shared axios instance used by every generated SDK call.
 * Call this once at boot; safe to re-call (it replaces the previous
 * instance).
 */
export function configureLalterClient(
  opts: LalterClientOptions,
): AxiosInstance {
  if (opts.axios) {
    sharedClient = opts.axios;
    sharedAuthHeader = undefined;
    return sharedClient;
  }
  sharedAuthHeader = `Bearer ${opts.apiKey}`;
  const instance = axios.create({
    baseURL: opts.baseURL,
    headers: { Authorization: sharedAuthHeader },
  });
  sharedClient = instance;
  return instance;
}

/**
 * The client configured by configureLalterClient, for callers that need the
 * raw axios instance — sendChatMessage uses this to stream the response
 * rather than decode it as one JSON value.
 *
 * Throws rather than falling back to an unconfigured default: unlike Spore,
 * lalter has no public base URL a caller could reach by accident, so a
 * missing configureLalterClient call must fail loudly instead of quietly
 * requesting the wrong host.
 */
export function getLalterClient(): AxiosInstance {
  if (!sharedClient) {
    throw new Error(
      "lalter SDK is not configured — call configureLalterClient() first",
    );
  }
  return sharedClient;
}

/**
 * The bearer header configureLalterClient set, for sendChatMessage's own
 * fetch() call. Undefined when configured with a custom axios instance —
 * that caller owns its own auth and sendChatMessage cannot recover it.
 */
export function getLalterAuthHeader(): string | undefined {
  return sharedAuthHeader;
}

/**
 * Mutator used by the generated orval client. Every generated function
 * routes through this so we can swap the underlying axios instance at
 * runtime via `configureLalterClient`.
 */
export const lalterHttp = async <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  const response: AxiosResponse<T> = await getLalterClient().request<T>({
    ...config,
    ...options,
    headers: {
      ...config.headers,
      ...options?.headers,
    },
  });
  return response.data;
};

export default lalterHttp;
