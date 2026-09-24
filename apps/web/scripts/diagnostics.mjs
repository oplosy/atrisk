import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);

const dependencyVersion = (name) => require(`${name}/package.json`).version;

const npmVersion = () => {
  const match = process.env.npm_config_user_agent?.match(/\bnpm\/([^\s]+)/);
  if (!match) {
    throw new Error(
      "npm_config_user_agent is required; run diagnostics via npm",
    );
  }
  return match[1];
};

export function formatDiagnostics(versions) {
  return [
    "atlasrisk web diagnostics",
    `runtime node ${versions.node}`,
    `package-manager npm ${versions.npm}`,
    `dependency react ${versions.react}`,
    `dependency react-dom ${versions.reactDom}`,
    `tool vite ${versions.vite}`,
    `tool vitest ${versions.vitest}`,
    `tool typescript ${versions.typescript}`,
  ].join("\n");
}

export function collectDiagnostics() {
  return formatDiagnostics({
    node: process.version,
    npm: npmVersion(),
    react: dependencyVersion("react"),
    reactDom: dependencyVersion("react-dom"),
    vite: dependencyVersion("vite"),
    vitest: dependencyVersion("vitest"),
    typescript: dependencyVersion("typescript"),
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  console.log(collectDiagnostics());
}
