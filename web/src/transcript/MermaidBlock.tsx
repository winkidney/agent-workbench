import { useContext, useEffect, useState } from "react";
import { Code2, Maximize2, Workflow } from "lucide-react";
import { CopyButton } from "./CopyButton";
import { useEnv } from "./media";
import {
  DiagramCacheContext,
  drawMermaid,
  MERMAID_MAX,
  StreamingContext,
  svgImage,
  useThemeKey,
  type Drawn,
} from "./mermaid";

// A ```mermaid fence: the diagram once the reply is complete, its source
// until then, if it is too large, or if it does not parse.
export function MermaidBlock({ code }: { code: string }) {
  const streaming = useContext(StreamingContext);
  const cache = useContext(DiagramCacheContext);
  const theme = useThemeKey();
  const env = useEnv();
  const wanted = !streaming && code.length <= MERMAID_MAX;
  const [drawn, setDrawn] = useState<Drawn | null>(() =>
    wanted ? (cache?.get(theme, code) ?? null) : null,
  );
  const [source, setSource] = useState(false);
  useEffect(() => {
    if (!wanted) return;
    const hit = cache?.get(theme, code);
    if (hit) {
      setDrawn(hit);
      return;
    }
    // Gone before its turn, or before it was drawn: nothing is kept for it.
    let gone = false;
    drawMermaid(code, theme, () => gone).then((d) => {
      if (!d || gone) return;
      cache?.set(theme, code, d);
      setDrawn(d);
    });
    return () => {
      gone = true;
    };
  }, [wanted, code, theme, cache]);

  const svg = wanted && drawn && "svg" in drawn ? drawn : null;
  const error = wanted && drawn && "error" in drawn ? drawn.error : null;
  const enlarge = () =>
    svg &&
    env.view(
      [
        {
          kind: "image",
          src: svgImage(svg.svg, svg.background),
          title: "Diagram",
        },
      ],
      0,
    );
  const showDiagram = svg && !source;
  return (
    <div className={"md-mermaid" + (showDiagram ? " drawn" : "")}>
      <div className="md-mermaid-tools">
        {svg && (
          <button
            type="button"
            className="copy-button"
            aria-label={source ? "Show the diagram" : "Show the source"}
            title={source ? "Diagram" : "Source"}
            onClick={() => setSource(!source)}
          >
            {source ? <Workflow size={12} /> : <Code2 size={12} />}
          </button>
        )}
        {showDiagram && (
          <button
            type="button"
            className="copy-button"
            aria-label="Enlarge the diagram"
            title="Enlarge"
            onClick={enlarge}
          >
            <Maximize2 size={12} />
          </button>
        )}
        <CopyButton text={code} label="Copy code" />
      </div>
      {showDiagram ? (
        <button
          type="button"
          className="md-mermaid-figure"
          aria-label="Diagram; enlarge"
          onClick={enlarge}
          // Sanitized by Mermaid (strict security level).
          dangerouslySetInnerHTML={{ __html: svg.svg }}
        />
      ) : (
        <pre>
          <code className="language-mermaid">{code}</code>
        </pre>
      )}
      {error && (
        <p className="md-mermaid-error">Diagram could not be drawn: {error}</p>
      )}
    </div>
  );
}
