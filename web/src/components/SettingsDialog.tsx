import { useEffect, useState } from "react";
import { Copy, Eye, EyeOff, RefreshCw, X } from "lucide-react";
import { api } from "../lib/api";
import {
  ACCESS_OPTIONS,
  DEFAULT_USERNAME,
  accessWarning,
  newPassword,
  type RemoteAccess,
} from "../lib/access";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Dialog, DialogContent, DialogTitle } from "./ui/dialog";

type Settings = {
  remoteAccess: RemoteAccess;
  localSignin: boolean;
  thisComputer: boolean;
  username?: string;
  storageBytes?: number;
  retentionDays?: number;
};
type Secrets = { pairingCode?: string; username?: string; password?: string };

// Copies, or selects the field where the clipboard is unavailable (plain
// HTTP on another device is not a secure context).
function copyField(id: string, value: string, done: () => void) {
  const select = () => {
    const el = document.getElementById(id) as HTMLInputElement | null;
    el?.focus();
    el?.select();
  };
  if (!navigator.clipboard) return select();
  navigator.clipboard.writeText(value).then(done, select);
}

export function SettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState<Settings>();
  const [secrets, setSecrets] = useState<Secrets>({});
  const [draft, setDraft] = useState<RemoteAccess>("off");
  const [localSignin, setLocalSignin] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [reveal, setReveal] = useState(false);
  const [copied, setCopied] = useState("");
  const [status, setStatus] = useState("");

  const load = async () => {
    const settings = await api<Settings>("settings");
    const sec = await api<Secrets>("settings/secrets");
    setSaved(settings);
    setSecrets(sec);
    setDraft(settings.remoteAccess);
    setLocalSignin(settings.localSignin);
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

  const save = async () => {
    setBusy(true);
    setError("");
    setStatus("");
    try {
      await api(
        "settings",
        "PUT",
        draft === "password"
          ? { remoteAccess: draft, localSignin, username, password }
          : { remoteAccess: draft, localSignin },
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
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) onOpenChange(next);
      }}
    >
      <DialogContent>
        <DialogTitle asChild>
          <h2>Settings</h2>
        </DialogTitle>
        <button
          className="modal-close"
          aria-label="Close settings"
          disabled={busy}
          onClick={() => onOpenChange(false)}
        >
          <X size={18} />
        </button>
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
                    title={
                      copied === "settings-pairing-code" ? "Copied" : "Copy"
                    }
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
          {saved?.storageBytes !== undefined && (
            <p className="subtle storage-line">
              Local data:{" "}
              {saved.storageBytes >= 1048576
                ? (saved.storageBytes / 1048576).toFixed(1) + " MB"
                : Math.ceil(saved.storageBytes / 1024) + " KB"}{" "}
              · live events are kept {saved.retentionDays ?? 7} days;
              conversations stay in each CLI's own records.
            </p>
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
          <Button type="submit" disabled={busy || !saved}>
            {busy ? (saved ? "Saving…" : "Loading…") : "Save settings"}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
