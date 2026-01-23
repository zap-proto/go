import type { Config } from 'tailwindcss'

const config: Config = {
  darkMode: 'class',
  content: [
    './app/**/*.{js,ts,jsx,tsx,mdx}',
    './content/**/*.{js,ts,jsx,tsx,mdx}',
    './node_modules/@hanzo/ui/**/*.{js,ts,jsx,tsx}',
    './node_modules/@hanzo/docs/**/*.{js,ts,jsx,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        primary: {
          DEFAULT: '#00D4AA',
          50: '#E6FFF9',
          100: '#B3FFE8',
          200: '#80FFD6',
          300: '#4DFFC5',
          400: '#1AFFB3',
          500: '#00D4AA',
          600: '#00A888',
          700: '#007D66',
          800: '#005244',
          900: '#002722',
        },
      },
      fontFamily: {
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      },
    },
  },
  plugins: [],
}

export default config
