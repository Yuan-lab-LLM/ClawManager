import { createRoot } from "react-dom/client";
import { InstanceServiceFrame } from "../../../src/components/InstanceServiceFrame";
import { I18nProvider } from "../../../src/contexts/I18nContext";
import "../../../src/index.css";

createRoot(document.getElementById("root")!).render(
  <I18nProvider>
    <div style={{ height: 600, maxWidth: 1100, margin: "24px auto" }}>
      <InstanceServiceFrame instanceId={42} instanceName="OpenClaw recovery fixture" instanceType="openclaw" instanceMode="lite" availability="available" />
    </div>
  </I18nProvider>,
);
