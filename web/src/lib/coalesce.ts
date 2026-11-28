// Runs fn one call at a time. A call while it runs asks for one more run
// after it, since what that run read may have changed since it began; more
// calls meanwhile join that one. So a burst of reasons to read the list
// again (an event, the page waking, the stream back) costs at most two
// reads, and the last one is never missed.
export function coalesced(fn: () => Promise<unknown>): () => void {
  let running = false;
  let again = false;
  const run = () => {
    if (running) {
      again = true;
      return;
    }
    running = true;
    void fn()
      .catch(() => {})
      .finally(() => {
        running = false;
        if (again) {
          again = false;
          run();
        }
      });
  };
  return run;
}
