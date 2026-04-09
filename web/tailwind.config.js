/** @type {import('tailwindcss').Config} */
export default {
  content: [
    './index.html',
    './src/**/*.{vue,js,ts,jsx,tsx}',
  ],
  theme: {
    extend: {
      colors: {
        navy: {
          800: '#1a1f36',
          900: '#141728',
        },
      },
    },
  },
  plugins: [],
}
