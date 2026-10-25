import { execSync } from "node:child_process";

// The about panel shows the version followed by CFBundleVersion in
// parentheses, which is the commit the app was built from.
const commit = execSync("git rev-parse --short HEAD", { encoding: "utf8" }).trim();

export default {
  name: "Mux",
  identifier: "dev.callcc.mux",
  version: "0.1.0",
  main: "./cmd/mux",
  out: "build",
  macos: {
    infoPlist: { CFBundleVersion: commit },
  },
};
