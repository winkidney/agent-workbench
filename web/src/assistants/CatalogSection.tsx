import { useEffect, useRef, useState } from "react";
import { Download, Loader2, RefreshCw } from "lucide-react";
import { api } from "../lib/api";
import {
  agentsOf,
  assistantsChanged,
  catalogName,
  type CatalogEntry,
  type MigrationItem,
} from "../lib/assistants";
import { AssistantIcon } from "../components/AssistantIcon";
import { AgentIcon } from "../components/AgentIcon";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Textarea } from "../components/ui/textarea";

type Agent = "claude" | "codex";

// Assistants from a catalog (ADR 0031): what it offers, Install, and for a
// repository made before catalogs, binding its assistants to it. The
// catalogs themselves are a list of git addresses.
export function CatalogSection() {
  const [sources, setSources] = useState<{ url: string }[] | null>(null);
  const [url, setUrl] = useState("");
  const [list, setList] = useState<CatalogEntry[] | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  // The latest request: an older answer (another catalog, an earlier
  // click) is dropped.
  const latest = useRef(0);
  const load = async (u: string) => {
    const n = ++latest.current;
    setLoading(true);
    setError("");
    try {
      const v = await api<{ catalog: string; assistants: CatalogEntry[] }>(
        `assistants/catalog?url=${encodeURIComponent(u)}`,
      );
      if (n === latest.current) setList(v.assistants ?? []);
    } catch (e) {
      if (n !== latest.current) return;
      setList(null);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (n === latest.current) setLoading(false);
    }
  };
  useEffect(() => {
    api<{ url: string }[]>("assistants/catalogs")
      .then((v) => {
        setSources(v);
        setUrl(v[0]?.url ?? "");
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)));
  }, []);
  const saveSources = async () => {
    const urls = draft
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
    try {
      const v = await api<{ url: string }[]>(
        "assistants/catalogs",
        "PUT",
        urls.map((u) => ({ url: u })),
      );
      setSources(v);
      setUrl(v[0]?.url ?? "");
      setList(null);
      setEditing(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };
  return (
    <section className="settings-section catalog-section">
      <h3>Add from a catalog</h3>
      <p className="subtle">
        A catalog is a git repository of assistants others share. An installed
        assistant's definition comes from it and is updated from it; your
        settings, state and records stay in your repository.
      </p>
      {sources && !editing && (
        <div className="catalog-bar">
          {sources.length > 1 ? (
            <select
              aria-label="Catalog"
              value={url}
              disabled={loading}
              onChange={(e) => {
                setUrl(e.target.value);
                setList(null);
              }}
            >
              {sources.map((s) => (
                <option key={s.url} value={s.url}>
                  {catalogName(s.url)}
                </option>
              ))}
            </select>
          ) : (
            <code>{catalogName(url)}</code>
          )}
          <Button
            variant="outline"
            size="sm"
            disabled={loading || !url}
            onClick={() => void load(url)}
          >
            {loading ? (
              <Loader2 size={14} className="spin" />
            ) : (
              <RefreshCw size={14} />
            )}
            {list ? "Check again" : "Browse"}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setDraft((sources ?? []).map((s) => s.url).join("\n"));
              setEditing(true);
            }}
          >
            Catalogs…
          </Button>
        </div>
      )}
      {editing && (
        <div className="catalog-edit">
          <label className="routine-field">
            <span className="routine-label">
              Catalogs: one git address per line
            </span>
            <Textarea
              rows={3}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
          </label>
          <div className="confirm-actions">
            <Button variant="ghost" size="sm" onClick={() => setEditing(false)}>
              Cancel
            </Button>
            <Button size="sm" onClick={() => void saveSources()}>
              Save
            </Button>
          </div>
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
      {list && (
        <ul className="catalog-list">
          {list.map((e) => (
            <CatalogItem key={e.path} entry={e} onInstalled={() => load(url)} />
          ))}
          {list.length === 0 && <li className="subtle">No assistants yet.</li>}
        </ul>
      )}
      {list && <BindSection url={url} onBound={() => void load(url)} />}
    </section>
  );
}

function CatalogItem({
  entry: e,
  onInstalled,
}: {
  entry: CatalogEntry;
  onInstalled: () => void;
}) {
  const agents = agentsOf(e);
  const [open, setOpen] = useState(false);
  const [dir, setDir] = useState(e.path);
  const [agent, setAgent] = useState<Agent>(
    agents.includes(e.agent) ? e.agent : agents[0],
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const install = async () => {
    setBusy(true);
    setError("");
    try {
      await api("assistants/install", "POST", {
        catalog: e.catalog,
        path: e.path,
        dir,
        agent,
      });
      assistantsChanged();
      setOpen(false);
      onInstalled();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <li className="catalog-item">
      <div className="catalog-item-head">
        <AssistantIcon
          icon={e.icon}
          agent={e.agent}
          size={32}
          badge={false}
          decorative
        />
        <div className="catalog-item-text">
          <strong>{e.name}</strong>
          {e.description && <p className="subtle">{e.description}</p>}
        </div>
        <span className="catalog-agents" title={`For ${agents.join(" and ")}`}>
          {agents.map((a) => (
            <AgentIcon key={a} agent={a} size={16} />
          ))}
        </span>
        {e.installed ? (
          <span className="subtle">Installed as {e.installed}</span>
        ) : (
          !open && (
            <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
              <Download size={14} />
              Install
            </Button>
          )
        )}
      </div>
      {open && (
        <div className="catalog-install">
          {!!e.requires?.length && (
            <p className="subtle">It needs: {e.requires.join("; ")}</p>
          )}
          <p className="subtle catalog-warn">
            Its scripts run on this computer. Its routines stay off until you
            set it up; read its procedure (AGENTS.md) first if you do not know
            its author.
          </p>
          <div className="catalog-install-row">
            <label className="routine-field">
              <span className="routine-label">Folder</span>
              <Input
                autoFocus
                value={dir}
                onChange={(ev) => setDir(ev.target.value)}
              />
            </label>
            {agents.length > 1 && (
              <div
                className="agent-switch"
                role="radiogroup"
                aria-label="Agent"
              >
                {agents.map((a) => (
                  <button
                    type="button"
                    role="radio"
                    key={a}
                    aria-checked={agent === a}
                    className={agent === a ? "on" : ""}
                    onClick={() => setAgent(a)}
                  >
                    <AgentIcon agent={a} size={16} decorative />
                    {a === "codex" ? "Codex" : "Claude"}
                  </button>
                ))}
              </div>
            )}
          </div>
          {agent === "codex" && e.codexWarning && (
            <p className="subtle catalog-warn">{e.codexWarning}</p>
          )}
          {agent === "codex" && (
            <p className="subtle">
              With Codex, its connectors are not turned off per assistant as
              Claude's settings do; its sandbox keeps it off the network.
            </p>
          )}
          {error && <p className="error-text">{error}</p>}
          <div className="confirm-actions">
            <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={busy || !dir.trim()}
              onClick={() => void install()}
            >
              {busy && <Loader2 size={14} className="spin" />}
              Install
            </Button>
          </div>
        </div>
      )}
    </li>
  );
}

// A repository made before catalogs: its assistants that the catalog also
// has are bound to it. Shown first as what would happen.
function BindSection({ url, onBound }: { url: string; onBound: () => void }) {
  const [plan, setPlan] = useState<MigrationItem[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const run = async (apply: boolean) => {
    setBusy(true);
    setError("");
    try {
      const v = await api<{ assistants: MigrationItem[] }>(
        "assistants/migrate",
        "POST",
        { catalog: url, apply },
      );
      setPlan(v.assistants);
      if (apply) {
        setDone(true);
        assistantsChanged();
        onBound();
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const bound = (plan ?? []).filter((i) => !i.skipped);
  return (
    <div className="catalog-bind">
      {!plan && (
        <Button
          variant="ghost"
          size="sm"
          disabled={busy}
          onClick={() => void run(false)}
        >
          Bind my existing assistants to this catalog…
        </Button>
      )}
      {plan && (
        <>
          <p className="subtle">
            {done
              ? "Bound. Files as the catalog has them left your repository; your changes and data stayed."
              : bound.length
                ? "Binding would do this; your data is not touched:"
                : "None of your assistants is in this catalog."}
          </p>
          <ul className="catalog-plan">
            {plan.map((i) => (
              <li key={i.dir}>
                <strong>{i.dir}</strong>:{" "}
                {i.skipped ? (
                  <span className="subtle">{i.skipped}</span>
                ) : (
                  <>
                    {i.fromCatalog?.length ?? 0} from the catalog
                    {!!i.changed?.length &&
                      `, yours kept: ${i.changed.join(", ")}`}
                    {!!i.added?.length && `, added: ${i.added.join(", ")}`}
                    {!!i.settings?.length &&
                      `, settings moved: ${i.settings.join(", ")}`}
                  </>
                )}
              </li>
            ))}
          </ul>
          {!done && bound.length > 0 && (
            <div className="confirm-actions">
              <Button variant="ghost" size="sm" onClick={() => setPlan(null)}>
                Cancel
              </Button>
              <Button size="sm" disabled={busy} onClick={() => void run(true)}>
                {busy && <Loader2 size={14} className="spin" />}
                Bind
              </Button>
            </div>
          )}
        </>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
