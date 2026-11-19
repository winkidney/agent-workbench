import { lazy, Suspense, type ComponentProps } from "react";
import type { AssistantHome as HomeT } from "./AssistantHome";
import type { AssistantSettings as SettingsT } from "./AssistantSettings";
import type { SetUpDialog as SetUpT } from "./SetUpDialog";
import type { ChangeBanner as BannerT, ChangeList as ListT } from "./Changes";
import type { RoutineDialog as RoutineT } from "../routines/RoutineDialog";
import type { AssistantsTab as TabT } from "../components/settings/AssistantsTab";

// Assistants' and routines' views load when first shown, not with the page:
// most visits never open them. Each is its own chunk, and what a view keeps
// is let go when it closes.
const Home = lazy(() =>
  import("./AssistantHome").then((m) => ({ default: m.AssistantHome })),
);
const Settings = lazy(() =>
  import("./AssistantSettings").then((m) => ({
    default: m.AssistantSettings,
  })),
);
const SetUp = lazy(() =>
  import("./SetUpDialog").then((m) => ({ default: m.SetUpDialog })),
);
const Banner = lazy(() =>
  import("./Changes").then((m) => ({ default: m.ChangeBanner })),
);
const List = lazy(() =>
  import("./Changes").then((m) => ({ default: m.ChangeList })),
);
const Routine = lazy(() =>
  import("../routines/RoutineDialog").then((m) => ({
    default: m.RoutineDialog,
  })),
);
const Tab = lazy(() =>
  import("../components/settings/AssistantsTab").then((m) => ({
    default: m.AssistantsTab,
  })),
);

export const AssistantHome = (p: ComponentProps<typeof HomeT>) => (
  <Suspense fallback={null}>
    <Home {...p} />
  </Suspense>
);
export const AssistantSettings = (p: ComponentProps<typeof SettingsT>) => (
  <Suspense fallback={null}>
    <Settings {...p} />
  </Suspense>
);
export const SetUpDialog = (p: ComponentProps<typeof SetUpT>) => (
  <Suspense fallback={null}>
    <SetUp {...p} />
  </Suspense>
);
export const ChangeBanner = (p: ComponentProps<typeof BannerT>) => (
  <Suspense fallback={null}>
    <Banner {...p} />
  </Suspense>
);
export const ChangeList = (p: ComponentProps<typeof ListT>) => (
  <Suspense fallback={null}>
    <List {...p} />
  </Suspense>
);
export const RoutineDialog = (p: ComponentProps<typeof RoutineT>) => (
  <Suspense fallback={null}>
    <Routine {...p} />
  </Suspense>
);
export const AssistantsTab = (p: ComponentProps<typeof TabT>) => (
  <Suspense fallback={null}>
    <Tab {...p} />
  </Suspense>
);
