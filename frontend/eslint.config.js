// @ts-check
const eslint = require("@eslint/js");
const tseslint = require("typescript-eslint");
const angular = require("@angular-eslint/eslint-plugin");
const angularTemplate = require("@angular-eslint/eslint-plugin-template");
const angularTemplateParser = require("@angular-eslint/template-parser");

module.exports = [
  {
    files: ["**/*.ts"],
    languageOptions: {
      parser: tseslint.parser,
      parserOptions: { project: ["tsconfig.json"] }
    },
    plugins: {
      "@typescript-eslint": tseslint.plugin,
      "@angular-eslint": angular
    },
    rules: {
      "@angular-eslint/directive-selector": [
        "error",
        { type: "attribute", prefix: "app", style: "camelCase" }
      ],
      "@angular-eslint/component-selector": [
        "error",
        { type: "element", prefix: "app", style: "kebab-case" }
      ]
    }
  },
  {
    files: ["**/*.html"],
    languageOptions: { parser: angularTemplateParser },
    plugins: { "@angular-eslint/template": angularTemplate },
    rules: {}
  }
];
