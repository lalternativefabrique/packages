import type { PlatformRateLimitConfig, PlatformRateLimitRule } from "./types"

// Better Auth keys its own rate limiter on NODE_ENV === "production", so a
// deployment that forgets the variable serves sign-in with no brute-force
// protection and nothing reports it. These are the platform's rules, applied
// regardless of the environment.
const PLATFORM_RULES: Record<string, PlatformRateLimitRule> = {
  "/sign-in/email": { window: 60, max: 5 },
  "/sign-up/email": { window: 60, max: 5 },
  "/email-otp/send-verification-otp": { window: 60, max: 3 },
  "/email-otp/verify-email": { window: 60, max: 5 },
  "/email-otp/reset-password": { window: 60, max: 5 },
  "/forget-password": { window: 60, max: 3 },
  "/reset-password": { window: 60, max: 5 },
  "/sign-in/magic-link": { window: 60, max: 3 },
  "/two-factor/verify-totp": { window: 60, max: 5 },
  "/two-factor/verify-otp": { window: 60, max: 5 },
  "/two-factor/verify-backup-code": { window: 60, max: 5 },
  "/two-factor/send-otp": { window: 60, max: 3 },
}

export interface ResolvedRateLimit {
  enabled: boolean
  window: number
  max: number
  storage?: "memory" | "database" | "secondary-storage"
  modelName?: string
  customRules: Record<string, PlatformRateLimitRule>
}

export function resolveRateLimit(
  config?: PlatformRateLimitConfig,
): ResolvedRateLimit {
  return {
    enabled: config?.enabled ?? true,
    window: config?.window ?? 10,
    max: config?.max ?? 100,
    ...(config?.storage ? { storage: config.storage } : {}),
    ...(config?.modelName ? { modelName: config.modelName } : {}),
    customRules: { ...PLATFORM_RULES, ...config?.customRules },
  }
}

export { PLATFORM_RULES }
