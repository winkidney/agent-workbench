import { Appearance } from "../Appearance";
import { NotifyToggle } from "../NotifyToggle";
import { SuggestToggle } from "../SuggestToggle";

// Preferences that apply at once: the same controls as the header's ⋯ menu.
export function GeneralTab() {
  return (
    <>
      <section className="settings-section">
        <h3>Appearance</h3>
        <p className="subtle">Remembered in this browser.</p>
        <Appearance />
      </section>
      <section className="settings-section">
        <h3>Notifications and suggestions</h3>
        <NotifyToggle />
        <SuggestToggle />
      </section>
    </>
  );
}
