const rsa = { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" };

const b64url = (bytes: Uint8Array) =>
  btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");

const part = (o: unknown) =>
  b64url(new TextEncoder().encode(JSON.stringify(o)));

export async function signer(kid = "k1") {
  const pair = (await crypto.subtle.generateKey(
    { ...rsa, modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]) },
    true,
    ["sign", "verify"],
  )) as CryptoKeyPair;
  const jwk = {
    ...(await crypto.subtle.exportKey("jwk", pair.publicKey)),
    kid,
    use: "sig",
  };
  const sign = async (payload: Record<string, unknown>) => {
    const head = `${part({ alg: "RS256", kid, typ: "JWT" })}.${part(payload)}`;
    const sig = await crypto.subtle.sign(
      rsa,
      pair.privateKey,
      new TextEncoder().encode(head),
    );
    return `${head}.${b64url(new Uint8Array(sig))}`;
  };
  return { jwk, sign };
}

export function unsigned(alg: string, payload: Record<string, unknown>) {
  return `${part({ alg, kid: "k1", typ: "JWT" })}.${part(payload)}.`;
}
