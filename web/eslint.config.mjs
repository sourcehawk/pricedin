import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTypeScript from "eslint-config-next/typescript";
import tseslint from "typescript-eslint";

export default defineConfig([
  globalIgnores([".next/**", "next-env.d.ts", "coverage/**"]),
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
