const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');

const SHARED_SOURCES = [
  'src/components/**/*.{ts,tsx}',
  'src/lib/**/*.{ts,tsx}',
  'src/hooks/**/*.{ts,tsx}',
  'src/constants/**/*.{ts,tsx}',
  'src/types/**/*.{ts,tsx}',
];

module.exports = defineConfig([
  expoConfig,
  { ignores: ['dist/*', 'android/*', 'ios/*', '.expo/*'] },
  {
    files: ['**/*.ts', '**/*.tsx'],
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: __dirname },
    },
    rules: {
      '@typescript-eslint/consistent-type-imports': 'warn',
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/await-thenable': 'error',
      '@typescript-eslint/no-floating-promises': 'warn',
      '@typescript-eslint/no-misused-promises': 'warn',
      '@typescript-eslint/require-await': 'warn',
      '@typescript-eslint/no-unnecessary-condition': 'warn',
      '@typescript-eslint/prefer-nullish-coalescing': 'warn',
      '@typescript-eslint/prefer-optional-chain': 'warn',
      '@typescript-eslint/switch-exhaustiveness-check': 'warn',
      'import/order': [
        'warn',
        { 'newlines-between': 'always', alphabetize: { order: 'asc', caseInsensitive: true } },
      ],
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      complexity: ['warn', 20],
      'max-depth': ['warn', 3],
      'max-lines': ['warn', { max: 600, skipBlankLines: true, skipComments: true }],
      'max-lines-per-function': ['warn', { max: 150, skipBlankLines: true, skipComments: true }],
      'max-params': ['warn', 4],
    },
  },
  {
    files: ['**/*.js'],
    languageOptions: {
      globals: { __dirname: 'readonly', __filename: 'readonly' },
    },
  },
  {
    files: ['**/*.d.ts'],
    rules: { 'import/order': 'off' },
  },
  {
    files: ['**/*.test.ts'],
    rules: {
      'max-lines': 'off',
      'max-lines-per-function': 'off',
      '@typescript-eslint/no-floating-promises': 'off',
    },
  },
  {
    files: ['src/features/checkpoint/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'warn',
        {
          patterns: [
            {
              group: ['@/features/auth/**', '@/features/profile/**'],
              message: 'Import another feature only through its public API.',
            },
          ],
        },
      ],
    },
  },
  {
    files: SHARED_SOURCES,
    rules: {
      'no-restricted-imports': [
        'warn',
        {
          patterns: [
            {
              group: ['@/features/**'],
              message:
                'Shared code must not depend on a feature; move the hook to lib/hooks or colocate it.',
            },
          ],
        },
      ],
    },
  },
]);
