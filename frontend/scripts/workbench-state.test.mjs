import assert from "node:assert/strict";
import { test } from "node:test";
import {
  initialWorkbench,
  workbenchReducer,
} from "../src/components/workbench/state.ts";

test("Agent commands and capability updates never create or select a preview", () => {
  const state = initialWorkbench(3);
  for (const event of [
    { type: "agent.command", command: "npm run dev http://localhost:3000" },
    { type: "capabilities", preview: true },
    { type: "instance.running" },
  ]) {
    assert.equal(workbenchReducer(state, event), state);
  }
  assert.equal(state.tabs.length, 0);
  assert.equal(state.panel, "files");
});
test("only explicit actions create tabs and switching/hiding retains sessions and previews", () => {
  let state = initialWorkbench(3);
  state = workbenchReducer(state, { type: "browser.new", id: "browser1" });
  state = workbenchReducer(state, {
    type: "browser.navigate",
    id: "browser1",
    address: "localhost:3000",
    url: "http://p-token.localhost:9001",
    generation: 3,
  });
  state = workbenchReducer(state, {
    type: "terminal.open",
    session: {
      id: "term1",
      generation: 3,
      name: "Terminal",
      cwd: "",
      exited: false,
      created_at: "now",
    },
  });
  state = workbenchReducer(state, { type: "tab.hide", id: "term1" });
  assert.equal(state.tabs.find((t) => t.id === "term1").session.id, "term1");
  assert.equal(state.tabs.find((t) => t.id === "term1").hidden, true);
  state = workbenchReducer(state, { type: "panel", panel: "git" });
  assert.equal(state.tabs[0].url, "http://p-token.localhost:9001");
  state = workbenchReducer(state, { type: "tab.restore", id: "term1" });
  assert.equal(state.active, "term1");
  assert.equal(state.panel, "preview");
});
test("migration fences old sessions and preview URLs, delayed old responses cannot reopen them", () => {
  let state = workbenchReducer(initialWorkbench(3), {
    type: "browser.new",
    id: "b",
  });
  state = workbenchReducer(state, { type: "generation", generation: 4 });
  state = workbenchReducer(state, {
    type: "browser.navigate",
    id: "b",
    address: "localhost:3000",
    url: "http://old-preview",
    generation: 3,
  });
  state = workbenchReducer(state, {
    type: "terminal.open",
    session: {
      id: "old",
      generation: 3,
      name: "Old",
      cwd: "",
      exited: false,
      created_at: "now",
    },
  });
  assert.equal(state.tabs.length, 1);
  assert.equal(state.tabs[0].url, undefined);
  assert.equal(state.tabs[0].stale, true);
});

test("tab storage is bounded and ending a view frees capacity", () => {
  let state = initialWorkbench(3);
  for (let i = 0; i < 32; i++)
    state = workbenchReducer(state, { type: "browser.new", id: `b${i}` });
  assert.equal(
    workbenchReducer(state, { type: "browser.new", id: "overflow" }),
    state,
  );
  assert.equal(
    workbenchReducer(state, {
      type: "terminal.open",
      session: {
        id: "overflow",
        generation: 3,
        name: "Terminal",
        cwd: "",
        exited: false,
        created_at: "now",
      },
    }),
    state,
  );
  state = workbenchReducer(state, { type: "tab.remove", id: "b0" });
  state = workbenchReducer(state, { type: "browser.new", id: "new" });
  assert.equal(state.tabs.length, 32);
  assert.equal(state.active, "new");
});

test("Git view patches preserve repository, diff and drafts across shared shells", () => {
  let state = initialWorkbench(3);
  assert.ok(state.git, "shared Git view state is missing");
  assert.equal(state.git.selected, null);
  state = workbenchReducer(state, {
    type: "git.patch",
    patch: {
      selected: "second",
      selection: { repo: "second", path: "other.txt", staged: false },
      mode: "history",
      message: "Commit draft",
      adding: "clone",
      path: "projects/draft",
      name: "Draft",
      url: "https://example.test/draft.git",
      creatingBranch: true,
      branch: "draft-branch",
      busy: true,
    },
  });
  const view = state.git;
  state = workbenchReducer(state, { type: "panel", panel: "preview" });
  state = workbenchReducer(state, { type: "git.patch", patch: { busy: false, error: "Retry" } });
  assert.deepEqual(state.git, { ...view, busy: false, error: "Retry" });
  assert.equal(state.panel, "preview");
  assert.equal(state.tabs.length, 0);
});
