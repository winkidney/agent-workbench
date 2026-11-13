// Live events applied once per animation frame (ADR 0001): queued as they
// come, run together. Reset forgets the frame it cancels, so the next event
// always schedules one; a frame left behind would hold every later event
// back until the page was reloaded.
export function frameBatch<T>(
  run: (items: T[]) => void,
  raf: (cb: () => void) => number = (cb) => requestAnimationFrame(cb),
  cancel: (id: number) => void = (id) => cancelAnimationFrame(id),
) {
  let queue: T[] = [];
  let frame = 0;
  const flush = () => {
    if (frame) cancel(frame);
    frame = 0;
    const items = queue;
    queue = [];
    if (items.length) run(items);
  };
  return {
    push(item: T) {
      queue.push(item);
      if (!frame)
        frame = raf(() => {
          frame = 0;
          flush();
        });
    },
    // Now, without waiting for a frame (a page shown again).
    flush,
    // Nothing pending, nothing scheduled.
    reset() {
      if (frame) cancel(frame);
      frame = 0;
      queue = [];
    },
    get pending() {
      return queue.length;
    },
  };
}
