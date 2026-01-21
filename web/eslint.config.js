import js from '@eslint/js';
import globals from 'globals';
import tseslint from 'typescript-eslint';
import reactPlugin from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import importPlugin from 'eslint-plugin-import';
import prettierConfig from 'eslint-config-prettier';

export default [
  { ignores: ['dist/**', 'node_modules/**'] },
  js.configs.recommended,
  // IMPORTANT:
  // We intentionally use the *non type-aware* TS ruleset here.
  // The type-aware presets (`recommendedTypeChecked`) are much stricter and will flag a lot of existing code;
  // they also require careful scoping + project configuration.
  ...tseslint.configs.recommended,
  reactPlugin.configs.flat.recommended,
  // Node-side tooling/config files (allow `console`, `process`, etc.)
  {
    files: ['eslint.config.js', 'scripts/**/*.{js,mjs,cjs}'],
    languageOptions: {
      globals: globals.node,
    },
  },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        project: ['./tsconfig.json'],
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
    settings: { react: { version: 'detect' } },
    plugins: {
      'react-refresh': reactRefresh,
      'react-hooks': reactHooks,
      import: importPlugin,
    },
    rules: {
      'react-refresh/only-export-components': 'warn',
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
    },
  },
  // React settings for JSX/TSX.
  { settings: { react: { version: 'detect' } } },
  prettierConfig,
];
