// @vitest-environment jsdom
import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it } from "vitest";

import { I18nProvider } from "../lib/i18n";
import { IconPlay } from "../lib/icons";
import { Button, IconBadge } from "./ui";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

async function classesOf(node: ReactNode): Promise<string> {
  await act(async () => root.render(<I18nProvider>{node}</I18nProvider>));
  return host.querySelector("button")!.className;
}

it("gives the filled and the bare buttons and the badges the hover tint", async () => {
  expect(await classesOf(<Button kind="secondary">Cancel</Button>)).toContain(
    "glim-btn-secondary",
  );
  expect(await classesOf(<Button kind="ghost">Details</Button>)).toContain(
    "glim-btn-ghost",
  );
  expect(
    await classesOf(<IconBadge icon={<IconPlay />} title="Start" />),
  ).toContain("glim-btn-secondary");
});

it("leaves the accent button, a pressed one and a verdict alone", async () => {
  for (const node of [
    <Button key="p">Save</Button>,
    <Button key="t" kind="secondary" pressed>
      Paused
    </Button>,
    <Button key="v" kind="secondary" verdict="ok">
      Test
    </Button>,
  ]) {
    expect(await classesOf(node)).not.toMatch(/glim-btn-(secondary|ghost)/);
  }
});
