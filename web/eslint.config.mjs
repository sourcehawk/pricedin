import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypeScript from "eslint-config-next/typescript";
import tseslint from "typescript-eslint";

// The engine is pure: the server's Go engine is its twin, and it must run
// anywhere, outside React and the browser too.
const engineNoUi = {
  group: ["react", "react/*", "react-dom", "react-dom/*", "next", "next/*", "@testing-library/*"],
  message: "The engine has no UI code: no React, no Next.js and no DOM.",
};
const engineNoApp = {
  group: ["@/*"],
  message: "The engine imports nothing from the rest of the app.",
};

export default defineConfig([
  globalIgnores([".next/**", "next-env.d.ts", "coverage/**", "src/api/schema.d.ts"]),
  ...nextVitals,
  ...nextTypeScript,
  {
    files: ["**/*.{ts,tsx,mts}"],
    extends: [tseslint.configs.strictTypeChecked],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "import/no-default-export": "error",
    },
  },
  {
    files: ["src/engine/**/*.ts"],
    rules: {
      "no-restricted-imports": ["error", { patterns: [engineNoUi, engineNoApp] }],
      "import/no-restricted-paths": [
        "error",
        {
          basePath: import.meta.dirname,
          zones: [
            {
              target: "./src/engine",
              from: "./src",
              except: ["./engine"],
              message: "The engine imports nothing from the rest of the app.",
            },
          ],
        },
      ],
      "no-restricted-globals": [
        "error",
        ...["window", "document", "navigator", "localStorage", "sessionStorage", "fetch"].map(
          (name) => ({
            name,
            message: "The engine has no DOM and no I/O.",
          }),
        ),
      ],
    },
  },
  {
    // Only the engine's tests read files.
    files: ["src/engine/**/*.ts"],
    ignores: ["src/engine/**/*.test.ts", "src/engine/testing/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            engineNoUi,
            engineNoApp,
            {
              group: ["node:*"],
              message: "The engine runs in the browser and has no I/O.",
            },
          ],
        },
      ],
      "import/no-nodejs-modules": "error",
    },
  },
  {
    // Next.js and the tools load these files through their default export.
    files: [
      "src/app/**/{page,layout,loading,error,not-found,template,default}.tsx",
      "*.config.{ts,mts}",
    ],
    rules: {
      "import/no-default-export": "off",
    },
  },
]);
