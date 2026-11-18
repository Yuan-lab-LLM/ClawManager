import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist-frame-recovery",
    rollupOptions: { input: "scripts/fixtures/instance-frame-recovery/index.html" },
  },
});
