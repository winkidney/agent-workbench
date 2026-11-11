// Access from other devices: this computer (localhost, 127.0.0.1) never
// signs in; other devices are refused, or sign in with the pairing code or
// with a username and password (HTTP Basic, the browser's own prompt).
export type RemoteAccess = "off" | "pairing" | "password";

export const ACCESS_OPTIONS: {
  id: RemoteAccess;
  label: string;
  hint: string;
}[] = [
  {
    id: "off",
    label: "Off",
    hint: "Only this computer can use the workbench.",
  },
  {
    id: "pairing",
    label: "Pairing code",
    hint: "Other devices enter the code shown here once; it stays valid until the workbench restarts, up to 12 hours.",
  },
  {
    id: "password",
    label: "Password",
    hint: "Other devices sign in with the browser's own username and password prompt, which browsers can remember.",
  },
];

export const DEFAULT_USERNAME = "workbench";

// 16 characters without look-alikes (no 0/o, 1/l/i), about 80 bits; easy to
// type on a phone. Same alphabet as the server's default.
export function newPassword(): string {
  const alphabet = "abcdefghjkmnpqrstuvwxyz23456789";
  const max = 256 - (256 % alphabet.length); // unbiased: drop the remainder
  let out = "";
  while (out.length < 16) {
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    for (const b of bytes)
      if (b < max && out.length < 16) out += alphabet[b % alphabet.length];
  }
  return out;
}

// What saving does to the device showing Settings. This computer skips
// sign-in unless "sign in on this computer too" is on; any other device (or
// this one, then) signs in with the chosen method.
export function accessWarning(o: {
  thisComputer: boolean;
  savedLocalSignin: boolean;
  localSignin: boolean;
  saved: RemoteAccess;
  draft: RemoteAccess;
  credentialsChanged: boolean;
}): string {
  if (o.thisComputer && !o.localSignin) return "";
  if (o.draft === "off")
    return "This device will be disconnected: only the workbench computer can turn access back on.";
  const signedInBefore = !(o.thisComputer && !o.savedLocalSignin);
  if (
    o.draft === "password" &&
    (!signedInBefore || o.saved !== "password" || o.credentialsChanged)
  )
    return o.thisComputer
      ? "This browser will ask for the username and password."
      : "This device will ask for the new username and password.";
  // Pairing: the server keeps the saving browser signed in.
  return "";
}
