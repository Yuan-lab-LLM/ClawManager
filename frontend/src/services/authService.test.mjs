import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";
import ts from "typescript";
import { create } from "zustand";

// Execute the production modules with an in-memory storage and API boundary.
// This needs no browser, network, credentials, or additional test dependency.
function loadModule(path, storage, dependencies) {
  const source = readFileSync(new URL(path, import.meta.url), "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  });
  const exports = {};
  runInNewContext(compiled.outputText, {
    exports,
    localStorage: storage,
    require: (name) => {
      if (!(name in dependencies)) throw new Error(`Unexpected dependency: ${name}`);
      return dependencies[name];
    },
  });
  return exports;
}

function setup(revocationError) {
  const values = new Map([["access_token", "fixture-access"], ["refresh_token", "fixture-refresh"]]);
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
  const calls = [];
  const api = { post: async (path) => {
    calls.push(path);
    assert.equal(storage.getItem("access_token"), "fixture-access", "The logout request still needs its bearer credential");
    if (revocationError) throw revocationError;
    return { data: { success: true } };
  } };
  const { authService } = loadModule("./authService.ts", storage, { "./api": { default: api } });
  const { useAuthStore } = loadModule("../stores/authStore.ts", storage, { zustand: { create }, "../services/authService": { authService } });
  return { authService, useAuthStore, storage, calls };
}

test("successful logout sends its credential before clearing both local tokens", async () => {
  const { authService, storage, calls } = setup();
  await authService.logout();
  assert.deepEqual(calls, ["/auth/logout"]);
  assert.equal(storage.getItem("access_token"), null);
  assert.equal(storage.getItem("refresh_token"), null);
});

test("503 revocation failure still clears local tokens and preserves the original rejection", async () => {
  const failure = Object.assign(new Error("Revocation unavailable"), { response: { status: 503 } });
  const { authService, storage } = setup(failure);
  await assert.rejects(authService.logout(), (error) => error === failure);
  assert.equal(storage.getItem("access_token"), null);
  assert.equal(storage.getItem("refresh_token"), null);
});

test("auth store leaves a visible incomplete-revocation notice after 503", async () => {
  const failure = new Error("Revocation unavailable");
  const { useAuthStore, storage } = setup(failure);
  useAuthStore.setState({ user: { id: 1 }, isAuthenticated: true });
  await assert.rejects(useAuthStore.getState().logout(), (error) => error === failure);
  const state = useAuthStore.getState();
  assert.equal(state.user, null);
  assert.equal(state.isAuthenticated, false);
  assert.equal(state.isLoading, false);
  assert.equal(state.error, "logout_incomplete");
  assert.equal(storage.getItem("access_token"), null);
});

test("successful auth-store logout does not show an incomplete-revocation warning", async () => {
  const { useAuthStore } = setup();
  useAuthStore.setState({ user: { id: 1 }, isAuthenticated: true, error: "old error" });
  await useAuthStore.getState().logout();
  assert.equal(useAuthStore.getState().isAuthenticated, false);
  assert.equal(useAuthStore.getState().error, null);
});
