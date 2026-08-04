import axios, {
  type AxiosInstance,
  type AxiosRequestConfig,
  type AxiosResponse,
} from "axios";

export interface SporeClientOptions {
  /**
   * Base URL of the Spore API. Defaults to https://api.sporee.fr.
   * Override for staging or local dev (e.g. http://localhost:4110).
   */
  baseURL?: string;

  /**
   * Bearer token. Accepts a managed `sk_live_*` API key, an HS256 JWT, or
   * the static API_KEY value. The SDK sends it as `Authorization: Bearer
   * <token>`.
   */
  apiKey?: string;

  /**
   * Optional custom axios instance. Use this to inject your own
   * interceptors, retry policy or telemetry. When provided, `baseURL` and
   * `apiKey` are ignored — wire those into your axios instance directly.
   */
  axios?: AxiosInstance;
}

let sharedClient: AxiosInstance | null = null;

/**
 * Configure the shared axios instance used by every generated SDK call.
 * Call this once at boot; safe to re-call (it replaces the previous
 * instance).
 */
export function configureSporeClient(opts: SporeClientOptions): AxiosInstance {
  if (opts.axios) {
    sharedClient = opts.axios;
    return sharedClient;
  }
  const instance = axios.create({
    baseURL: opts.baseURL ?? "https://api.sporee.fr",
    headers: opts.apiKey
      ? { Authorization: `Bearer ${opts.apiKey}` }
      : undefined,
  });
  sharedClient = instance;
  return instance;
}

function getClient(): AxiosInstance {
  if (!sharedClient) {
    sharedClient = axios.create({ baseURL: "https://api.sporee.fr" });
  }
  return sharedClient;
}

/**
 * Mutator used by the generated orval client. Every generated function
 * routes through this so we can swap the underlying axios instance at
 * runtime via `configureSporeClient`.
 */
export const sporeHttp = async <T>(
  config: AxiosRequestConfig,
  options?: AxiosRequestConfig,
): Promise<T> => {
  const response: AxiosResponse<T> = await getClient().request<T>({
    ...config,
    ...options,
    headers: {
      ...config.headers,
      ...options?.headers,
    },
  });
  return response.data;
};

export default sporeHttp;
