// How the sidebar folds a long list of sessions: a project's, an
// assistant's, a session's subsessions, a project's related sessions and the
// Other groups all show their newest LIST_SHOWN first, Show more adds
// LIST_STEP at a time and Show less folds them back. The open session and
// every session that needs the user stay listed past the limit, and a search
// shows every match. Pinned and Active are not folded: what is there was
// pinned, or runs or waits for the user.
export const LIST_SHOWN = 5;
export const LIST_STEP = 15;

// The items a folded list shows: the first limit, and those it keeps (the
// open session, the ones that need the user) even past it, in their order;
// all of them while searching.
export function limited<T>(
  items: T[],
  limit: number,
  keeps?: (item: T) => boolean,
  all = false,
): T[] {
  if (all) return items;
  const shown = items.slice(0, limit);
  return keeps ? [...shown, ...items.slice(limit).filter(keeps)] : shown;
}

// The Show more and Show less buttons under a folded list whose rows sit at
// depth (indented to line up with them).
export function ShowMoreLess({
  total,
  limit,
  setLimit,
  shown = Math.min(total, limit),
  depth = 1,
  all = false,
}: {
  total: number;
  limit: number;
  setLimit: (n: number) => void;
  // How many rows the list shows now (limited's length: those kept past
  // the limit are already there); the first limit when left out.
  shown?: number;
  depth?: number;
  // Searching: every item is shown, nothing to fold.
  all?: boolean;
}) {
  if (all) return null;
  const style = { paddingLeft: 36 + (depth - 1) * 12 };
  return (
    <>
      {total > shown && (
        <button
          type="button"
          className="show-more"
          style={style}
          onClick={() => setLimit(limit + LIST_STEP)}
        >
          Show {Math.min(LIST_STEP, total - shown)} more
        </button>
      )}
      {limit > LIST_SHOWN && total > LIST_SHOWN && (
        <button
          type="button"
          className="show-more"
          style={style}
          onClick={() => setLimit(LIST_SHOWN)}
        >
          Show less
        </button>
      )}
    </>
  );
}
