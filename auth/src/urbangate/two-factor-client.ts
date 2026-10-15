import type {
  BackupCodes,
  Result,
  TotpEnrolment,
  TwoFactorStatus,
  UrbangateAuthClient,
} from "./client.ts";

type Call = <T = unknown>(
  method: "GET" | "POST",
  path: string,
  body?: unknown,
) => Promise<Result<T>>;

export function twoFactorClient(call: Call): UrbangateAuthClient["twoFactor"] {
  return {
    status: () => call<TwoFactorStatus>("GET", "two-factor"),
    startTotp: (input = {}) =>
      call<TotpEnrolment>("POST", "two-factor/totp/start", input),
    confirmTotp: (input) => call("POST", "two-factor/totp/confirm", input),
    disableTotp: (input = {}) => call("POST", "two-factor/totp/disable", input),
    backupCodes: (input = {}) =>
      call<BackupCodes>("POST", "two-factor/backup-codes", input),
    confirmBackupCodes: (input = {}) =>
      call("POST", "two-factor/backup-codes/confirm", input),
  };
}
