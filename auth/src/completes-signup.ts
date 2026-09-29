const SIGNUP_COMPLETING = [
  "/sign-up/email",
  "/sign-in/email-otp",
  "/email-otp/verify-email",
  "/callback/",
]

// Matched on the path rather than a response body: the three Better Auth
// sign-up flows return three different shapes.
export function completesSignup(pathname: string): boolean {
  return SIGNUP_COMPLETING.some((p) => pathname.includes(p))
}
