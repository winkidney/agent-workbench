import { useEffect, useState } from "react";
import { Copy, Eye, EyeOff, LogOut, RefreshCw } from "lucide-react";
import { api } from "../../lib/api";
import {
  ACCESS_OPTIONS,
  DEFAULT_USERNAME,
  accessWarning,
  newPassword,
  originBlocked,
  parseOrigins,
  type RemoteAccess,
} from "../../lib/access";
import { connection } from "../../lib/about";
import { copyField } from "../../lib/copy";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

type Settings = {
  remoteAccess: RemoteAccess;
  localSignin: boolean;
  thisComputer: boolean;
  allowedOrigins: string[];
  username?: string;
  signedIn?: number;
};
type Secrets = { pairingCode?: string; username?: string; password?: string };

// Access & security: how this page is connected, who else may connect and
// how they sign in, and the devices signed in.
export function AccessTab({
  open,
  onDirty,
  onBusy,
}: {
  open: boolean;
  onDirty: (dirty: boolean) => void;
  onBusy: (busy: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState<Settings>();
  const [secrets, setSecrets] = useState<Secrets>({});
  const [draft, setDraft] = useState<RemoteAccess>("off");
  const [localSignin, setLocalSignin] = useState(false);
  const [origins, setOrigins] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [reveal, setReveal] = useState(false);
  const [copied, setCopied] = useState("");
  const [status, setStatus] = useState("");
  const [signedOut, setSignedOut] = useState("");

  const load = async () => {
    const settings = await api<Settings>("settings");
    const sec = await api<Secrets>("settings/secrets");
    setSaved(settings);
    setSecrets(sec);
    setDraft(settings.remoteAccess);
    setLocalSignin(settings.localSignin);
    setOrigins(settings.allowedOrigins.join("\n"));
    // Defaults the first time; saved only when the password is chosen.
    setUsername(sec.username ?? DEFAULT_USERNAME);
    setPassword(sec.password ?? newPassword());
  };
  useEffect(() => {
    if (!open) {
      // Secrets leave the page with the dialog.
      setSecrets({});
      setPassword("");
      setReveal(false);
      return;
    }
    setBusy(true);
    setError("");
    setStatus("");
    setSignedOut("");
    load()
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  }, [open]);

  const credentialsChanged =
    username !== (secrets.username ?? "") ||
    password !== (secrets.password ?? "");
  const warning = saved
    ? accessWarning({
        thisComputer: saved.thisComputer,
        savedLocalSignin: saved.localSignin,
        localSignin,
        saved: saved.remoteAccess,
        draft,
        credentialsChanged: draft === "password" && credentialsChanged,
      })
    : "";

  const dirty =
    !!saved &&
    (draft !== saved.remoteAccess ||
      localSignin !== saved.localSignin ||
      origins.trim() !== saved.allowedOrigins.join("\n") ||
      (draft === "password" && credentialsChanged));
  useEffect(() => onDirty(dirty), [dirty]);
  useEffect(() => onBusy(busy), [busy]);
  useEffect(() => () => onDirty(false), []);
  const originRefused =
    !!saved &&
    originBlocked(location.origin, location.host, saved.allowedOrigins);

  const signOutOthers = async () => {
    if (
      !confirm(
        "Sign out every other device? They will need the pairing code again.",
      )
    )
      return;
    setBusy(true);
    setError("");
    try {
      const v = await api<{ ended: number; settings: Settings }>(
        "settings/signout-others",
        "POST",
      );
      setSaved(v.settings);
      setSignedOut(
        v.ended === 1
          ? "Signed out 1 device."
          : `Signed out ${v.ended} devices.`,
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const allowedOrigins = parseOrigins(origins);
      await api(
        "settings",
        "PUT",
        draft === "password"
          ? {
              remoteAccess: draft,
              localSignin,
              allowedOrigins,
              username,
              password,
            }
          : { remoteAccess: draft, localSignin, allowedOrigins },
      );
      await load();
      setStatus(
        draft === "off"
          ? "Saved. Only this computer can use the workbench."
          : "Saved.",
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const copy = (id: string, value: string) =>
    copyField(id, value, () => {
      setCopied(id);
      setTimeout(() => setCopied(""), 1200);
    });

  return (
    <>
      {saved && (
        <ul className="connection-card" aria-label="This connection">
          {connection({
            thisComputer: saved.thisComputer,
            localSignin: saved.localSignin,
            remoteAccess: saved.remoteAccess,
            signedIn: saved.signedIn,
            protocol: location.protocol,
            originRefused,
          }).map((l) => (
            <li key={l.text} className={l.warn ? "warn" : undefined}>
              {l.text}
            </li>
          ))}
        </ul>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <fieldset className="access-setting" disabled={busy || !saved}>
          <legend>Access from other devices</legend>
          {ACCESS_OPTIONS.map((o) => (
            <label key={o.id} className="access-option">
              <input
                type="radio"
                name="remote-access"
                value={o.id}
                checked={draft === o.id}
                onChange={() => {
                  setDraft(o.id);
                  // Without a sign-in method nobody could get in.
                  if (o.id === "off") setLocalSignin(false);
                }}
              />
              <span>
                <strong>{o.label}</strong>
                <small>{o.hint}</small>
              </span>
            </label>
          ))}
        </fieldset>
        <label className="access-option local-signin">
          <input
            type="checkbox"
            checked={localSignin}
            disabled={busy || !saved || draft === "off"}
            onChange={(e) => setLocalSignin(e.target.checked)}
          />
          <span>
            <strong>Sign in on this computer too</strong>
            <small>
              {draft === "off"
                ? "Needs a pairing code or a password."
                : "Turn on when a tunnel or reverse proxy on this computer (ssh -R, frp, ngrok, nginx…) makes the workbench reachable from elsewhere: its visitors arrive from this computer. If you lose the code or password, they are in the data directory (pairing-code, access-password.json)."}
            </small>
          </span>
        </label>
        <p className="subtle access-note">
          {localSignin
            ? "This computer signs in too."
            : "This computer (localhost, 127.0.0.1) never signs in."}
        </p>
        {draft === "pairing" &&
          saved?.remoteAccess === "pairing" &&
          secrets.pairingCode && (
            <div className="access-secret">
              <label htmlFor="settings-pairing-code">Pairing code</label>
              <div className="secret-row">
                <Input
                  id="settings-pairing-code"
                  readOnly
                  value={secrets.pairingCode}
                  onFocus={(e) => e.target.select()}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label="Copy pairing code"
                  title={copied === "settings-pairing-code" ? "Copied" : "Copy"}
                  onClick={() =>
                    copy("settings-pairing-code", secrets.pairingCode!)
                  }
                >
                  <Copy size={15} />
                </Button>
              </div>
            </div>
          )}
        {draft === "password" && (
          <div className="access-secret">
            <label htmlFor="settings-username">Username</label>
            <Input
              id="settings-username"
              autoComplete="off"
              value={username}
              maxLength={64}
              onChange={(e) => setUsername(e.target.value)}
              required
            />
            <label htmlFor="settings-password">Password</label>
            <div className="secret-row">
              <Input
                id="settings-password"
                type={reveal ? "text" : "password"}
                autoComplete="new-password"
                value={password}
                minLength={8}
                maxLength={128}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={reveal ? "Hide password" : "Show password"}
                title={reveal ? "Hide" : "Show"}
                onClick={() => setReveal(!reveal)}
              >
                {reveal ? <EyeOff size={15} /> : <Eye size={15} />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label="Copy password"
                title={copied === "settings-password" ? "Copied" : "Copy"}
                onClick={() => {
                  setReveal(true);
                  copy("settings-password", password);
                }}
              >
                <Copy size={15} />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label="Generate a new password"
                title="Generate"
                onClick={() => {
                  setPassword(newPassword());
                  setReveal(true);
                }}
              >
                <RefreshCw size={15} />
              </Button>
            </div>
            <p className="subtle">
              On plain HTTP the password crosses your network unencrypted; on
              networks you don't trust, use a VPN such as Tailscale.
            </p>
          </div>
        )}
        {draft !== "off" && (
          <div className="access-secret">
            <label htmlFor="settings-origins">Allowed origins</label>
            <textarea
              id="settings-origins"
              className="origins-input"
              rows={2}
              spellCheck={false}
              autoCapitalize="off"
              placeholder="https://ai.example.com"
              value={origins}
              onChange={(e) => setOrigins(e.target.value)}
            />
            <p className="subtle">
              Other addresses you open this workbench from, one per line, for
              example an HTTPS tunnel or reverse proxy. Pages there may make
              changes once signed in; pages on any other site may not. List only
              addresses that lead to this workbench. This computer, while it
              skips sign-in, never takes changes from them.
            </p>
          </div>
        )}
        {warning && (
          <p role="alert" className="access-warning">
            {warning}
          </p>
        )}
        {error && (
          <p role="alert" className="error-text">
            {error}
          </p>
        )}
        {status && (
          <p role="status" className="subtle">
            {status}
          </p>
        )}
        <Button type="submit" disabled={busy || !saved || !dirty}>
          {busy ? (saved ? "Saving…" : "Loading…") : "Save access settings"}
        </Button>
      </form>
      {saved?.remoteAccess === "pairing" && (
        <section className="settings-section signed-in">
          <h3>Signed-in devices</h3>
          <div className="signed-in-row">
            <span>
              {saved.signedIn === 1
                ? "1 device is signed in with the pairing code."
                : `${saved.signedIn ?? 0} devices are signed in with the pairing code.`}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy || !saved.signedIn}
              onClick={() => void signOutOthers()}
            >
              <LogOut size={14} />
              Sign out other devices
            </Button>
          </div>
          {signedOut && (
            <p role="status" className="subtle">
              {signedOut}
            </p>
          )}
        </section>
      )}
      {saved?.remoteAccess === "password" && (
        <p className="subtle">
          Browsers remember the password for each device; a new password signs
          every device out.
        </p>
      )}
    </>
  );
}
