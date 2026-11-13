import type { Page } from "@playwright/test";
import type { RuntimePodRegisterRequest } from "../fixtures/apiClient.js";
import { expect } from "../fixtures/test.js";

export class AdminRuntimePodsPage {
  constructor(private readonly page: Page) {}

  private main() {
    return this.page.getByRole("main");
  }

  private podRow(podName: string) {
    return this.main().getByRole("row").filter({ hasText: podName }).first();
  }

  private formatRuntimeType(runtimeType: RuntimePodRegisterRequest["runtime_type"]) {
    return runtimeType === "hermes" ? "Hermes" : "OpenClaw";
  }

  async goto() {
    await this.page.goto("/admin/runtime-pods");
  }

  async expectVisible() {
    await expect(this.page.getByRole("navigation", { name: "Current page", exact: true })).toBeVisible();
  }

  async expectEmptyState() {
    await this.expectVisible();
    await expect(this.main().getByText("No Pods match the filters")).toBeVisible();
  }

  async selectRuntimeFilter(label: "All" | "OpenClaw" | "Hermes") {
    await this.main().getByRole("button", { name: /^Runtime type:/ }).click();
    await this.page.getByRole("option", { name: label, exact:true }).click();
  }

  async expectPodVisible(podName: string) {
    await this.expectVisible();
    const row = this.podRow(podName);

    await expect(row).toBeVisible();
    await expect(row.getByRole("button", { name: podName, exact: true })).toBeVisible();
  }

  async expectPodStatusAndCapacity(pod: RuntimePodRegisterRequest) {
    await this.expectPodVisible(pod.pod_name);
    const row = this.podRow(pod.pod_name);

    await expect(row.getByText(this.formatRuntimeType(pod.runtime_type), { exact: true })).toBeVisible();
    await expect(row.locator('.runtime-status')).toHaveText('Online');
    await expect(row.getByRole('img', {name:new RegExp(`Unknown ${pod.capacity}`)})).toBeVisible();
    await expect(row.getByText(new RegExp(` / ${pod.capacity}$`))).toBeVisible();
  }

  async expectPodMetricsVisible(pod: RuntimePodRegisterRequest) {
    const podName = pod.pod_name;
    const row = this.podRow(podName);

    await row.getByRole("button", { name: podName, exact: true }).click();
    await expect(this.main().getByText("CPU", { exact: true }).first()).toBeVisible();
    await expect(this.main().getByText("Memory", { exact: true }).first()).toBeVisible();
    await expect(this.main().getByRole('heading', { name: podName, exact: true })).toBeVisible();
    const metadata = this.main().getByText(/^Node:/);
    await expect(metadata).toContainText(pod.node_name!);
    await expect(metadata).toContainText(pod.pod_ip!);
    await expect(metadata).toContainText(`${pod.capacity} Agent`);
    await expect(this.main().getByRole('columnheader',{name:'CPU',exact:true})).toBeVisible();
    await expect(this.main().getByRole('columnheader',{name:'Memory',exact:true})).toBeVisible();
  }
}
